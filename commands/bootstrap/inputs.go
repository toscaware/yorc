// Copyright 2018 Bull S.A.S. Atos Technologies - Bull, Rue Jean Jaures, B.P.68, 78340, Les Clayes-sous-Bois, France.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bootstrap

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ystia/yorc/rest"

	"github.com/ystia/yorc/tosca"

	"github.com/spf13/viper"
	"github.com/ystia/yorc/commands"
	"github.com/ystia/yorc/config"

	"gopkg.in/AlecAivazis/survey.v1"
	"gopkg.in/yaml.v2"
)

var inputValues TopologyValues

// Default values for inputs
type defaultInputType struct {
	description string
	value       interface{}
}

var (
	ansibleDefaultInputs = map[string]defaultInputType{
		"ansible.version": defaultInputType{
			description: "Ansible version",
			value:       ansibleVersion,
		},
	}

	yorcDefaultInputs = map[string]defaultInputType{
		"yorc.download_url": defaultInputType{
			description: "Yorc download URL",
			value:       getYorcDownloadURL(),
		},
		"yorc.port": defaultInputType{
			description: "Yorc HTTP REST API port",
			value:       config.DefaultHTTPPort,
		},
		"yorc.data_dir": defaultInputType{
			description: "Yorc Home directory",
			value:       "/var/yorc",
		},
	}

	yorcPluginDefaultInputs = map[string]defaultInputType{
		"yorc_plugin.download_url": defaultInputType{
			description: "Yorc plugin download URL",
			value:       getYorcPluginDownloadURL(),
		},
	}

	alien4CloudDefaultInputs = map[string]defaultInputType{
		"alien4cloud.download_url": defaultInputType{
			description: "Alien4Cloud download URL",
			value: fmt.Sprintf(
				"https://fastconnect.org/maven/content/repositories/opensource/alien4cloud/alien4cloud-dist/%s/alien4cloud-dist-%s-dist.tar.gz",
				alien4cloudVersion, alien4cloudVersion),
		},
		"alien4cloud.port": defaultInputType{
			description: "Alien4Cloud port",
			value:       8088,
		},
		"alien4cloud.user": defaultInputType{
			description: "Alien4Cloud user",
			value:       "admin",
		},
		"alien4cloud.password": defaultInputType{
			description: "Alien4Cloud password",
			value:       "admin",
		},
	}

	consulDefaultInputs = map[string]defaultInputType{
		"consul.download_url": defaultInputType{
			description: "Consul download URL",
			value: fmt.Sprintf("https://releases.hashicorp.com/consul/%s/consul_%s_linux_amd64.zip",
				consulVersion, consulVersion),
		},
		"consul.port": defaultInputType{
			description: "Consul port",
			value:       8500,
		},
	}

	terraformDefaultInputs = map[string]defaultInputType{
		"terraform.download_url": defaultInputType{
			description: "Terraform download URL",
			value: fmt.Sprintf("https://releases.hashicorp.com/terraform/%s/terraform_%s_linux_amd64.zip",
				terraformVersion, terraformVersion),
		},
		"terraform.plugins_download_urls": defaultInputType{
			description: "Terraform plugins dowload URLs",
			value:       getTerraformPluginsDownloadURLs(),
		},
	}

	jdkDefaultInputs = map[string]defaultInputType{
		"jdk.download_url": defaultInputType{
			description: "Java Development Kit download URL",
			value:       "https://edelivery.oracle.com/otn-pub/java/jdk/8u131-b11/d54c1d3a095b4ff2b6607d096fa80163/jdk-8u131-linux-x64.tar.gz",
		},
		"jdk.version": defaultInputType{
			description: "Java Development Kit version",
			value:       "1.8.0-131-b11",
		},
	}
)

// Infrastructure inputs, which when missing, will have to be provided interactively
type infrastructureInputType struct {
	Name         string
	Description  string
	Required     bool
	Secret       bool        `yaml:"secret,omitempty"`
	DataType     string      `yaml:"type"`
	DefaultValue interface{} `yaml:"default,omitempty"`
}

type userInputType struct {
	Infrastructure map[string][]infrastructureInputType
}

// setDefaultInputValues sets environment variables and command line bootstrap options
// with default values
func setDefaultInputValues() error {

	defaultInputs := []map[string]defaultInputType{
		ansibleDefaultInputs,
		yorcDefaultInputs,
		yorcPluginDefaultInputs,
		alien4CloudDefaultInputs,
		consulDefaultInputs,
		terraformDefaultInputs,
		jdkDefaultInputs,
	}

	for _, defaultInput := range defaultInputs {

		for key, input := range defaultInput {
			flatKey := strings.Replace(key, ".", "_", 1)
			switch input.value.(type) {
			case string:
				bootstrapCmd.PersistentFlags().String(flatKey, input.value.(string), input.description)
			case int:
				bootstrapCmd.PersistentFlags().Int(flatKey, input.value.(int), input.description)
			case []string:
				bootstrapCmd.PersistentFlags().StringArray(flatKey, input.value.([]string), input.description)
			default:
				return fmt.Errorf("Unexpected default value type for %s value %v", key, input.value)

			}

			bootstrapViper.BindPFlag(key, bootstrapCmd.PersistentFlags().Lookup(flatKey))
			bootstrapViper.BindEnv(key,
				strings.ToUpper(fmt.Sprintf("%s_%s",
					environmentVariablePrefix, flatKey)))
			bootstrapViper.SetDefault(key, input.value)
		}
	}

	return nil
}

// initializeInputs Initializes parameters from environment variables, CLI options,
// input file in argument, and asks for user input if needed
func initializeInputs(inputFilePath, resourcesPath string, configuration config.Configuration) error {

	var err error
	inputValues, err = getInputValues(inputFilePath)
	if err != nil {
		return err
	}

	// Get infrastructure from viper configuration if not provided in inputs
	if inputValues.Infrastructure == nil {
		inputValues.Infrastructure = configuration.Infrastructures
	}
	// Now check for missing mandatory parameters and ask them to the user

	if infrastructureType == "" {

		// if one and only one infrastructure is already defined in inputs,
		// selecting this infrastructure
		if len(inputValues.Infrastructure) == 1 && len(inputValues.Hosts) == 0 {
			for k := range inputValues.Infrastructure {
				infrastructureType = k
			}
		} else if len(inputValues.Infrastructure) == 0 && len(inputValues.Hosts) > 0 {
			infrastructureType = "hostspool"
		}

		if infrastructureType == "" {
			fmt.Println("")
			prompt := &survey.Select{
				Message: "Select an infrastructure:",
				Options: []string{"Google", "AWS", "OpenStack", "HostsPool"},
			}
			survey.AskOne(prompt, &infrastructureType, nil)
			infrastructureType = strings.ToLower(infrastructureType)
		}
	}

	// Define node types to get according to the selecting infrastructure
	var infraNodeType, networkNodeType string
	switch infrastructureType {
	case "openstack":
		infraNodeType = "org.ystia.yorc.pub.infrastructure.OpenStackConfig"
		networkNodeType = "yorc.nodes.openstack.FloatingIP"
	case "google":
		infraNodeType = "org.ystia.yorc.pub.infrastructure.GoogleConfig"
		networkNodeType = "yorc.nodes.google.Address"
	case "aws":
		infraNodeType = "org.ystia.yorc.pub.infrastructure.AWSConfig"
		networkNodeType = "yorc.nodes.aws.PublicNetwork"
	case "hostspool":
		// No infrastructure defined in case of hosts pool
		// Hosts Pool don't have network-reletad on-demand resources
	default:
		return fmt.Errorf("Infrastruture type %s is not supported by bootstrap",
			infrastructureType)
	}

	// Get the infrastructure definition from resources
	nodeTypesFilePathPattern := filepath.Join(
		resourcesPath, "topology", "org.ystia.yorc.pub", "*", "types.yaml")

	matchingPath, err := filepath.Glob(nodeTypesFilePathPattern)
	if err != nil {
		return err
	}
	if len(matchingPath) == 0 {
		return fmt.Errorf("Found no node types definition file matching pattern %s", nodeTypesFilePathPattern)
	}

	data, err := ioutil.ReadFile(matchingPath[0])
	if err != nil {
		return err
	}
	var topology tosca.Topology
	if err := yaml.Unmarshal(data, &topology); err != nil {
		return err
	}

	// Update Yorc private key content if needed
	if inputValues.Yorc.PrivateKeyContent == "" {

		fmt.Println("\nGetting Yorc configuration")

		if inputValues.Yorc.PrivateKeyFile == "" {
			answer := struct {
				Value string
			}{}

			prompt := &survey.Input{
				Message: "Path to ssh private key accessible locally (will be stored as .ssh/yorc.pem on bootstrapped Yorc server Home):",
			}
			question := &survey.Question{
				Name:     "value",
				Prompt:   prompt,
				Validate: survey.Required,
			}
			if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
				return err
			}

			inputValues.Yorc.PrivateKeyFile = strings.TrimSpace(answer.Value)
		}

		data, err := ioutil.ReadFile(inputValues.Yorc.PrivateKeyFile)
		if err != nil {
			return err
		}
		inputValues.Yorc.PrivateKeyContent = string(data[:])
	} else {
		// Store this content locally to be used by the local Yorc
		// when bootstrapping the remote Yorc
		inputValues.Yorc.PrivateKeyFile = filepath.Join(resourcesPath, "yorc.pem")
		err = ioutil.WriteFile(inputValues.Yorc.PrivateKeyFile, []byte(inputValues.Yorc.PrivateKeyContent), 0700)
		if err != nil {
			return err
		}

	}

	fmt.Println("\nGetting Infrastructure configuration")

	askIfNotRequired := false
	convertBooleanToString := false
	// Get infrastructure inputs, except in the Hosts Pool case as Hosts Pool
	// doesn't have any infrastructure property
	if infrastructureType != "hostspool" {
		if inputValues.Infrastructure == nil {
			askIfNotRequired = true
			inputValues.Infrastructure = make(map[string]config.DynamicMap)
		}
		configMap := inputValues.Infrastructure[infrastructureType]
		if configMap == nil {
			configMap = make(config.DynamicMap)
		}

		if err := getResourceInputs(topology, infraNodeType, askIfNotRequired,
			convertBooleanToString, &configMap); err != nil {
			return err
		}
		inputValues.Infrastructure[infrastructureType] = configMap
	} else {

		// Hosts Pool
		if inputValues.Hosts == nil {
			askIfNotRequired = true
			inputValues.Hosts = make([]rest.HostConfig, 0)
			inputValues.Hosts, err = getHostsInputs(resourcesPath)
			if err != nil {
				return err
			}
		}
	}

	// Get on-demand resources definition for this infrastructure type
	onDemandResourceName := fmt.Sprintf("yorc-%s-types.yml", infrastructureType)
	data, err = tosca.Asset(onDemandResourceName)
	if err := yaml.Unmarshal(data, &topology); err != nil {
		return err
	}

	// First get on-demand compute nodes inputs
	// If the user didn't yet provided any property value,
	// let him the ability to specify any property.
	// If the user has already provided some property values,
	// jus asking missing required values

	fmt.Println("\nGetting Compute instances configuration")

	askIfNotRequired = false
	if inputValues.Compute == nil {
		askIfNotRequired = true
		inputValues.Compute = make(config.DynamicMap)
	}
	nodeType := fmt.Sprintf("yorc.nodes.%s.Compute", infrastructureType)
	// On-demand resources boolean values need to be passed to
	// Alien4Cloud REST API as strings
	convertBooleanToString = true
	if err := getResourceInputs(topology, nodeType, askIfNotRequired,
		convertBooleanToString, &inputValues.Compute); err != nil {
		return err
	}

	// Get Compute credentials, not on Hosts Pool as Hosts credentials are provided
	// in the Hosts Pool configuration
	convertBooleanToString = false
	if infrastructureType != "hostspool" {
		fmt.Println("\nGetting Compute instances credentials")

		askIfNotRequired = false
		if inputValues.Credentials == nil {
			askIfNotRequired = true
			var creds CredentialsConfiguration
			inputValues.Credentials = &creds
		}
		if err := getComputeCredentials(askIfNotRequired, inputValues.Credentials); err != nil {
			return err
		}
	}

	// Network connection
	if networkNodeType != "" {

		fmt.Println("\nGetting Network configuration")
		askIfNotRequired = false
		if inputValues.Address == nil {
			askIfNotRequired = true
			inputValues.Address = make(config.DynamicMap)
		}
		if err := getResourceInputs(topology, networkNodeType, askIfNotRequired,
			convertBooleanToString, &inputValues.Address); err != nil {
			return err
		}
	}

	// Fill in uninitialized values
	inputValues.Location.ResourcesFile = filepath.Join("resources",
		fmt.Sprintf("ondemand_resources_%s.yaml", infrastructureType))
	switch infrastructureType {
	case "openstack":
		inputValues.Location.Type = "OpenStack"
	case "google":
		inputValues.Location.Type = "Google Cloud"
	case "aws":
		inputValues.Location.Type = "AWS"
	case "hostspool":
		inputValues.Location.Type = "HostsPool"
	default:
		return fmt.Errorf("Bootstrapping on %s not supported yet", infrastructureType)
	}

	inputValues.Location.Name = inputValues.Location.Type

	if reviewInputs {
		err = reviewAndUpdateInputs()
	}
	return err
}

// reviewAndUpdateInputs allows the user to review and change deployment inputs
// if an editor could be found, else the review is skipped
func reviewAndUpdateInputs() error {

	bSlice, err := yaml.Marshal(inputValues)
	if err != nil {
		return err
	}

	// Avoid failures on missing editor, following survey package logic
	// checking environment variables, or if we can find a usual editor
	var editor string
	editorFound := (os.Getenv("VISUAL") != "") || (os.Getenv("EDITOR") != "")
	if !editorFound {
		editor, err = exec.LookPath("vim")
		if err != nil {
			editor, err = exec.LookPath("vi")
		}

		editorFound = (err == nil)
	}

	if !editorFound {
		fmt.Printf("Skipping review and update as no editor was found (should define EDITOR environment variable)")
		return nil
	}

	prompt := &survey.Editor{
		Message:       "Review and update inputs if needed",
		Default:       string(bSlice[:]),
		HideDefault:   true,
		AppendDefault: true,
	}

	if editor != "" {
		prompt.Editor = editor
	}

	var reply string
	err = survey.AskOne(prompt, &reply, nil)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal([]byte(reply), &inputValues)

	return nil
}

// getResourceInputs asks for input parameters of an infrastructure or on-demand
// resource
func getComputeCredentials(askIfNotRequired bool, creds *CredentialsConfiguration) error {

	answer := struct {
		Value string
	}{}

	if creds.User == "" {
		prompt := &survey.Input{
			Message: "User used to connect to Compute instances:",
		}
		question := &survey.Question{
			Name:     "value",
			Prompt:   prompt,
			Validate: survey.Required,
		}
		if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
			return err
		}
		creds.User = answer.Value
	}

	if askIfNotRequired {
		prompt := &survey.Input{
			Message: "Private key:",
		}
		question := &survey.Question{
			Name:   "value",
			Prompt: prompt,
		}
		if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
			return err
		}
		creds.Keys = make(map[string]string)
		creds.Keys["0"] = answer.Value

	}

	return nil
}

// getHostsInputs asks for input parameters of Hosts to add in pool
func getHostsInputs(resourcesPath string) ([]rest.HostConfig, error) {

	fmt.Println("\nDefining hosts to add in the Pool, SSH connections to these hosts will be done using Yorc private key")

	answer := struct {
		Value string
	}{}

	var result []rest.HostConfig
	finished := false
	for !finished {
		prompt := &survey.Input{
			Message: "Name identifying the new host to add in pool:"}

		question := &survey.Question{
			Name:     "value",
			Prompt:   prompt,
			Validate: survey.Required,
		}
		if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
			return nil, err
		}
		var hostConfig rest.HostConfig
		hostConfig.Name = answer.Value

		prompt = &survey.Input{
			Message: fmt.Sprintf("Hostname or ip address used to connect to the host (default: %s):",
				hostConfig.Name)}

		question = &survey.Question{
			Name:   "value",
			Prompt: prompt,
		}
		if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
			return nil, err
		}

		if answer.Value != "" {
			hostConfig.Connection.Host = answer.Value
		} else {
			hostConfig.Connection.Host = hostConfig.Name
		}

		prompt = &survey.Input{
			Message: "User used to connect to the host (default: root):"}

		question = &survey.Question{
			Name:   "value",
			Prompt: prompt,
		}
		if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
			return nil, err
		}

		if answer.Value != "" {
			hostConfig.Connection.User = answer.Value
		} else {
			hostConfig.Connection.User = "root"
		}

		prompt = &survey.Input{
			Message: "SSH port used to connect to the host (default: 22):"}

		question = &survey.Question{
			Name:   "value",
			Prompt: prompt,
			Validate: func(val interface{}) error {
				// if the input matches the expectation
				str := val.(string)
				if str != "" {
					if _, err := strconv.ParseUint(answer.Value, 10, 64); err != nil {
						return fmt.Errorf("You entered %s, not an unsigned integer", str)
					}
				}
				return nil
			},
		}
		if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
			return nil, err
		}

		if answer.Value != "" {
			port, _ := strconv.ParseUint(answer.Value, 10, 64)
			hostConfig.Connection.Port = port
		} else {
			hostConfig.Connection.Port = 22
		}

		fmt.Println("Defining key/value labels for this host, a public_address label should be defined")
		hostConfig.Labels = make(map[string]string)
		labelsFinished := false
		publicAddressDefined := false
		for !labelsFinished {

			prompt := &survey.Input{
				Message: "Label key:"}

			question := &survey.Question{
				Name:     "value",
				Prompt:   prompt,
				Validate: survey.Required,
			}
			if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
				return nil, err
			}
			labelKey := answer.Value

			if labelKey == "public_address" {
				publicAddressDefined = true
			}

			prompt = &survey.Input{
				Message: "Label value:"}

			question = &survey.Question{
				Name:   "value",
				Prompt: prompt,
			}
			if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
				return nil, err
			}

			hostConfig.Labels[labelKey] = answer.Value

			// If the public address label is alreay defined
			// asking if a new label must be defined
			// else a new label must be defined, because public_address is
			// mandatory for the bootstrap
			if publicAddressDefined {
				promptEnd := &survey.Select{
					Message: "Add another key/value label:",
					Options: []string{"yes", "no"},
					Default: "no",
				}
				var reply string
				survey.AskOne(promptEnd, &reply, nil)
				labelsFinished = (reply == "no")
			}

		}

		// The private SSH key used to connect to the host is the Yorc private key
		hostConfig.Connection.PrivateKey = filepath.Join(inputValues.Yorc.DataDir, ".ssh", "yorc.pem")

		// Forge component expect the label os.type to be linux,
		// if no such label is defined, adding it
		if _, ok := hostConfig.Labels["os.type"]; !ok {
			hostConfig.Labels["os.type"] = "linux"
		}

		result = append(result, hostConfig)

		fmt.Println("Host added to the Pool.")
		// Ask if another host has to be defined
		promptEnd := &survey.Select{
			Message: "Add another host in pool:",
			Options: []string{"yes", "no"},
			Default: "no",
		}
		var reply string
		survey.AskOne(promptEnd, &reply, nil)
		if reply == "no" {
			finished = true
		}
	}
	fmt.Printf("\n%d hosts added to the pool\n", len(result))
	return result, nil

}

// getResourceInputs asks for input parameters of an infrastructure or on-demand
// resource
func getResourceInputs(topology tosca.Topology, resourceName string,
	askIfNotRequired bool,
	convertBooleanToString bool,
	resultMap *config.DynamicMap) error {

	nodeType, found := topology.NodeTypes[resourceName]
	if !found {
		return fmt.Errorf("Unknown node type %s", resourceName)
	}

	for propName, definition := range nodeType.Properties {

		// Check if a value is already provided before asking for user input
		// ot if the value is not required
		required := definition.Required != nil && *definition.Required
		if resultMap.IsSet(propName) ||
			(!askIfNotRequired && !required) {
			continue
		}

		description := getFormattedDescription(definition.Description)

		if definition.Type == "boolean" {
			defaultValue := "false"
			if definition.Default != nil {
				defaultValue = definition.Default.GetLiteral()
			}
			prompt := &survey.Select{
				Message: fmt.Sprintf("%s:", description),
				Options: []string{"true", "false"},
				Default: defaultValue,
			}
			var answer string
			survey.AskOne(prompt, &answer, nil)
			if convertBooleanToString {
				resultMap.Set(propName, answer)
			} else {
				value := false
				if answer == "true" {
					value = true
				}
				resultMap.Set(propName, value)
			}

		} else {

			isList := (definition.Type == "list")
			answer := struct {
				Value string
			}{}

			var additionalMsg string
			if isList {
				additionalMsg = "(comma-separated list"

			}
			if definition.Default != nil {
				if additionalMsg != "" {
					additionalMsg = fmt.Sprintf(", default: %v", definition.Default.GetLiteral())
				} else {
					additionalMsg = fmt.Sprintf("(default: %v", definition.Default.GetLiteral())
				}
			}
			if additionalMsg != "" {
				additionalMsg += ")"
			}

			var prompt survey.Prompt
			loweredProp := strings.ToLower(propName)
			if strings.Contains(loweredProp, "password") ||
				strings.Contains(loweredProp, "secret") {
				prompt = &survey.Password{
					Message: fmt.Sprintf("%s%s:",
						description,
						additionalMsg)}
			} else {
				prompt = &survey.Input{
					Message: fmt.Sprintf("%s%s:",
						description,
						additionalMsg)}
			}
			question := &survey.Question{
				Name:   "value",
				Prompt: prompt,
			}
			if required {
				question.Validate = survey.Required
			}
			if err := survey.Ask([]*survey.Question{question}, &answer); err != nil {
				return err
			}

			if answer.Value != "" {
				if !strings.Contains(answer.Value, ",") {
					resultMap.Set(propName, answer.Value)
				} else {
					value := strings.Split(answer.Value, ",")
					for i, val := range value {
						value[i] = strings.TrimSpace(val)
					}
					resultMap.Set(propName, value)
				}
			} else if definition.Default != nil {
				resultMap.Set(propName, definition.Default.GetLiteral())
			}
		}
	}

	return nil

}

// getFormattedDescription reformats the description found in yaml node types
// definitions
func getFormattedDescription(description string) string {
	result := strings.TrimPrefix(description, "The ")
	result = strings.TrimPrefix(result, "the ")
	result = strings.TrimPrefix(result, "A ")
	result = strings.TrimSpace(result)
	result = strings.TrimSuffix(result, ".")
	return result
}

// getInputValues initializes topology values from an input file
func getInputValues(inputFilePath string) (TopologyValues, error) {

	var values TopologyValues

	if inputFilePath != "" {

		// A file was provided, merge its values definitions
		bootstrapViper.SetConfigFile(inputFilePath)

		if err := bootstrapViper.MergeInConfig(); err != nil {
			_, ok := err.(viper.ConfigFileNotFoundError)
			if inputFilePath != "" && !ok {
				fmt.Println("Can't use config file:", err)
			}
		}
	}

	err := bootstrapViper.Unmarshal(&values)
	return values, err
}

// getTerraformPluginsDownloadURLs return URLs where to download Terraform plugins
// sed by the orchestrator
func getTerraformPluginsDownloadURLs() []string {
	urlFormat := "https://releases.hashicorp.com/terraform-provider-%s/%s/terraform-provider-%s_%s_linux_amd64.zip"

	pluginVersionMap := map[string]string{
		"null":      "1.0.0",
		"consul":    commands.TfConsulPluginVersion,
		"google":    commands.TfGooglePluginVersion,
		"openstack": commands.TfOpenStackPluginVersion,
		"aws":       commands.TfAWSPluginVersion,
	}

	var pluginURLs []string
	for plugin, version := range pluginVersionMap {
		pluginURLs = append(pluginURLs, fmt.Sprintf(urlFormat, plugin, version, plugin, version))
	}

	return pluginURLs
}

// getYorcDownloadURL returns Yorc download URL,
// either a snapshot download or a release/milestone URL
func getYorcDownloadURL() string {
	var downloadURL string
	if strings.Contains(yorcVersion, "SNAPSHOT") {
		downloadURL = fmt.Sprintf(
			"https://dl.bintray.com/ystia/yorc-engine/snapshots/develop/yorc-%s.tgz",
			yorcVersion)
	} else {
		downloadURL = fmt.Sprintf(
			"https://github.com/ystia/yorc/releases/download/v%s/yorc-%s.tgz",
			yorcVersion, yorcVersion)
	}
	return downloadURL
}

// getYorcPluginDownloadURL returns Yorc plugin download URL,
// either a snapshot download or a release/milestone URL
func getYorcPluginDownloadURL() string {
	var downloadURL string
	if strings.Contains(yorcVersion, "SNAPSHOT") {
		downloadURL = fmt.Sprintf(
			"https://dl.bintray.com/ystia/yorc-a4c-plugin/snapshots/develop/alien4cloud-yorc-plugin-%s.zip",
			yorcVersion)
	} else {
		downloadURL = fmt.Sprintf(
			"https://github.com/ystia/yorc-a4c-plugin/releases/download/v%s/alien4cloud-yorc-plugin-%s.zip",
			yorcVersion, yorcVersion)
	}
	return downloadURL
}
