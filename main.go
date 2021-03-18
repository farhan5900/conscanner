package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	mapset "github.com/deckarep/golang-set"
	"github.com/go-yaml/yaml"
	log "github.com/sirupsen/logrus"
	"github.com/thatisuday/commando"
)

type ImagesEntry struct {
	Scheme   string `json:"scheme"`
	Registry string `json:"registry"`
	Image    string `json:"image"`
	Tag      string `json:"tag"`
}

type ImagesJson struct {
	Images []ImagesEntry `json:"images"`
}

// Function to find all the files that have `yaml` or `yml` as an extension in the provided source directory.
func find_yamls(src_dir string) []string {
	var yaml_files []string

	err := filepath.Walk(src_dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			r, e := regexp.MatchString(`\.yaml|\.yml`, info.Name())
			if e == nil && r {
				yaml_files = append(yaml_files, path)
			}
		}
		return nil
	})

	if err != nil {
		log.Errorf("conscanner: Unable to process directory (%s): %v\n", src_dir, err)
		os.Exit(1)
	}

	return yaml_files
}

// Function to extract all the mentioned docker images in the source yaml files. It is based on the pattern which follow the below grammar.
// There is a chance that it can match with any other URL or part of URL, which is not exactly a docker image.
// That is why we need validate function to remove invalid docker images.
//
//  reference                       := name [ ":" tag ] [ "@" digest ]
//  name                            := [hostname '/'] component ['/' component]*
//  hostname                        := hostcomponent ['.' hostcomponent]* [':' port-number]
//  hostcomponent                   := /([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])/
//  port-number                     := /[0-9]+/
//  component                       := alpha-numeric [separator alpha-numeric]*
//  alpha-numeric                   := /[a-z0-9]+/
//  separator                       := /[_.]|__|[-]*/
//
//  tag                             := /[\w][\w.-]{0,127}/
//
//  digest                          := digest-algorithm ":" digest-hex
//  digest-algorithm                := digest-algorithm-component [ digest-algorithm-separator digest-algorithm-component ]
//  digest-algorithm-separator      := /[+.-_]/
//  digest-algorithm-component      := /[A-Za-z][A-Za-z0-9]*/
//  digest-hex                      := /[0-9a-fA-F]{32,}/ ; At least 128 bit digest value
func extract_images_by_pattern(images mapset.Set, yaml_files []string) {
	re := regexp.MustCompile(`(([a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])|[a-zA-Z0-9])(\.(([a-zA-Z0-9][a-zA-Z0-9-]*[a-zA-Z0-9])|[a-zA-Z0-9]))*(:[0-9]+)?/([a-z0-9]+((_|\.|__|(-)*)[a-z0-9]+)*)(/[a-z0-9]+((_|\.|__|(-)*)[a-z0-9]+)*)*:(\w(\w|\.|-){0,127})(@[A-Za-z][A-Za-z0-9]*([+.-_][A-Za-z][A-Za-z0-9]*)?:[0-9a-fA-F]{32,})?`)

	for _, fp := range yaml_files {
		content, err := ioutil.ReadFile(fp)
		if err != nil {
			log.Errorf("conscanner: Unable to read file (%s): %v\n", fp, err)
			continue
		}

		for _, img := range re.FindAllString(string(content), -1) {
			images.Add(img)
		}
	}
}

// Function to iterate over the content body provided and look for `image` field
func image_lookup(images mapset.Set, body interface{}) interface{} {
	switch x := body.(type) {
	case map[interface{}]interface{}:
		m2 := map[interface{}]interface{}{}
		for k, v := range x {
			if k.(string) == "image" {
				reg := ""
				rep := ""
				tag := ""
				vref := reflect.ValueOf(v)
				for _, kk := range vref.MapKeys() {
					vv := vref.MapIndex(kk)
					if fmt.Sprintf("%s", kk) == "registry" {
						reg = fmt.Sprintf("%s", vv)
					} else if fmt.Sprintf("%s", kk) == "repository" {
						rep = fmt.Sprintf("%s", vv)
					} else if fmt.Sprintf("%s", kk) == "tag" {
						tag = fmt.Sprintf("%s", vv)
					}
				}
				images.Add(fmt.Sprintf("%s/%s:%s", reg, rep, tag))
			}
			m2[k] = image_lookup(images, v)
		}
		return m2
	case []interface{}:
		for i, v := range x {
			x[i] = image_lookup(images, v)
		}
	}
	return body
}

// Function to extract all the mentioned docker images in the source yaml files. It is based on the following YAML structure appearing anywhere in the file.
// image:
//   registry: docker.io
//   repository: mesosphere/chart
//   tag: t1
func extract_images_by_fields(images mapset.Set, yaml_files []string) {
	for _, fp := range yaml_files {
		var body interface{}
		content, err := ioutil.ReadFile(fp)
		if err != nil {
			log.Errorf("conscanner: Unable to read file (%s): %v\n", fp, err)
			continue
		}

		if err := yaml.Unmarshal(content, &body); err != nil {
			log.Warningf("conscanner: Unable to load YAML file (%s): %v\n", fp, err)
			continue
		}

		image_lookup(images, body)
	}
}

// Function to validate all the images in the image set provided. The validation is done using `curl` command.
// It also arranges image information into different fields, so that it can be written into a JSON file.
func validate_images(image_set mapset.Set) ImagesJson {
	it := image_set.Iterator()
	var img_json = ImagesJson{Images: []ImagesEntry{}}

	for img_int := range it.C {
		img_name := fmt.Sprintf("%v", img_int)
		log.Infof("conscanner: Validating Docker Image: %s", img_name)

		sch := "https"
		reg := strings.Split(img_name, "/")[0]
		if reg == "localhost" || strings.Contains(reg, ".") || strings.Contains(reg, ":") {
			img_name = img_name[len(reg)+1:]
		} else {
			reg = "docker.io"
		}
		img := strings.Split(img_name, ":")[0]
		tag := strings.Split(img_name, ":")[1]

		cmd := exec.Command("curl", "--silent", "-f", "-lSL", sch+"://hub.docker.com/v2/repositories/"+img+"/tags/"+tag)
		if _, err := cmd.Output(); err != nil {
			log.Warningf("conscanner: Invalid Image or Image Does Not Exists: %v\n", err)
		} else {
			log.Infoln("conscanner: Docker Image is valid!")
			img_json.Images = append(img_json.Images, ImagesEntry{Scheme: sch, Registry: reg, Image: img, Tag: tag})
		}
	}
	it.Stop()
	return img_json
}

// Function to find all the images available in the provided source directory.
// It only consideres YAML/YML files. Output would be generated in a JSON file name `images.json`
func find_images(args map[string]commando.ArgValue, flags map[string]commando.FlagValue) {
	log.Infoln("conscanner: Running for finding images...")

	image_set := mapset.NewSet()
	yaml_files := find_yamls(args["dir"].Value)
	extract_images_by_pattern(image_set, yaml_files)
	extract_images_by_fields(image_set, yaml_files)
	image_json := validate_images(image_set)
	json_file, _ := json.MarshalIndent(image_json, "", "")

	ioutil.WriteFile("images.json", json_file, 0644)

	log.Infoln("conscanner: Successfully Generated `images.json` file")
}

// Function to generate report for each image present in images.json file.
// It uses `grype` command line and save reports in a JSON files inside `conscanner-reports` directory
func gen_report(args map[string]commando.ArgValue, flags map[string]commando.FlagValue) {
	log.Infoln("conscanner: Running for generating report...")

	report_dir := "conscanner-reports"
	if err := os.Mkdir(report_dir, 0755); err != nil {
		log.Errorf("conscanner: Unable to create directory (%s): %v\n", report_dir, err)
		os.Exit(1)
	}

	image_file := args["image-file"].Value
	var image_json = ImagesJson{Images: []ImagesEntry{}}
	json_file, _ := ioutil.ReadFile(image_file)

	if err := json.Unmarshal(json_file, &image_json); err != nil {
		log.Errorf("conscanner: Unable to process file (%s): %v\n", image_file, err)
		os.Exit(1)
	}

	for _, image_entry := range image_json.Images {
		image := image_entry.Image + ":" + image_entry.Tag
		log.Infof("conscanner: Generating Report for docker image: %s", image)
		cmd := exec.Command("grype", "--quiet", "--output", "json", image)

		if resp, err := cmd.Output(); err != nil {
			log.Errorf("conscanner: Unable to run report generating command (%s): %v\n", cmd.String(), err)
		} else {
			report_file := report_dir + "/" + image_entry.Scheme + "_" + strings.ReplaceAll(image_entry.Registry, ":", "_") + "_" + strings.ReplaceAll(image_entry.Image, "/", "_") + "_" + image_entry.Tag + ".json"
			ioutil.WriteFile(report_file, resp, 0644)
			log.Infof("conscanner: Successfully generated report `%s` for docker image: %s", report_file, image)
		}
	}
	log.Infoln("conscanner: Done.")
}

func main() {

	// configure command
	commando.
		SetExecutableName("conscanner").
		SetVersion("1.0.0").
		SetDescription("A Tool that can find the docker images in a directory and do the reporting on all the images.")

	// configure the images command
	commando.
		Register("images").
		SetShortDescription("Look for docker images in a directory or file").
		SetDescription("This command will find the docker images in a directory or file and generate images.json file").
		AddArgument("dir", "Source directory path containing all the YAML files", "./").
		SetAction(find_images)

	// configure the report command
	commando.
		Register("report").
		SetShortDescription("Run CVE Scanning for all the images").
		SetDescription("This command will read all the docker images in a images.json file and generate report for each image").
		AddArgument("image-file", "Path for images.json file", "./images.json").
		SetAction(gen_report)

	commando.Parse(nil)

	os.Exit(0)
}
