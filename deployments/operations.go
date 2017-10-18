package deployments

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v2"
	"novaforge.bull.com/starlings-janus/janus/prov"
	"novaforge.bull.com/starlings-janus/janus/tosca"

	"github.com/hashicorp/consul/api"
	"github.com/pkg/errors"

	"novaforge.bull.com/starlings-janus/janus/helper/consulutil"
)

// IsOperationNotImplemented checks if a given error is an error indicating that an operation is not implemented
func IsOperationNotImplemented(err error) bool {
	err = errors.Cause(err)
	_, ok := err.(operationNotImplemented)
	return ok
}

type operationNotImplemented struct {
	msg string
}

func (oni operationNotImplemented) Error() string {
	return oni.msg
}

// IsInputNotFound checks if a given error is an error indicating that an input was not found in an operation
func IsInputNotFound(err error) bool {
	err = errors.Cause(err)
	_, ok := err.(inputNotFound)
	return ok
}

type inputNotFound struct {
	inputName          string
	operationName      string
	implementationType string
}

func (inf inputNotFound) Error() string {
	return fmt.Sprintf("input %q not found for operation %q implemented in type %q", inf.inputName, inf.operationName, inf.implementationType)
}

const implementationArtifactsExtensionsPath = "implementation_artifacts_extensions"

// GetOperationPathAndPrimaryImplementationForNodeType traverses the type hierarchy to find an operation matching the given operationName.
//
// Once found it returns the path to the operation and the value of its primary implementation.
// If the operation is not found in the type hierarchy then empty strings are returned.
func GetOperationPathAndPrimaryImplementationForNodeType(kv *api.KV, deploymentID, nodeType, operationName string) (string, string, error) {
	// First check if operation exists in current nodeType
	operationPath := getOperationPath(deploymentID, nodeType, operationName)
	kvp, _, err := kv.Get(path.Join(operationPath, "implementation/primary"), nil)
	if err != nil {
		return "", "", errors.Wrapf(err, "Failed to retrieve primary implementation for operation %q on type %q", operationName, nodeType)
	}
	if kvp != nil && len(kvp.Value) > 0 {
		return operationPath, string(kvp.Value), nil
	}

	// Not found here check the type hierarchy
	parentType, err := GetParentType(kv, deploymentID, nodeType)
	if err != nil || parentType == "" {
		return "", "", err
	}

	return GetOperationPathAndPrimaryImplementationForNodeType(kv, deploymentID, parentType, operationName)
}

// This function return the path for a given operation
func getOperationPath(deploymentID, nodeType, operationName string) string {
	var op string
	if idx := strings.Index(operationName, "configure."); idx >= 0 {
		op = operationName[idx:]
	} else if idx := strings.Index(operationName, "standard."); idx >= 0 {
		op = operationName[idx:]
	} else if idx := strings.Index(operationName, "custom."); idx >= 0 {
		op = operationName[idx:]
	} else {
		op = strings.TrimPrefix(operationName, "tosca.interfaces.node.lifecycle.")
		op = strings.TrimPrefix(op, "tosca.interfaces.relationship.")
		op = strings.TrimPrefix(op, "tosca.interfaces.node.lifecycle.")
		op = strings.TrimPrefix(op, "tosca.interfaces.relationship.")
	}
	op = strings.Replace(op, ".", "/", -1)
	operationPath := path.Join(consulutil.DeploymentKVPrefix, deploymentID, "topology/types", nodeType, "interfaces", op)

	return operationPath

}

// GetRelationshipTypeImplementingAnOperation  returns the first (bottom-up) type in the type hierarchy of a given relationship that implements a given operation
//
// An error is returned if the operation is not found in the type hierarchy
func GetRelationshipTypeImplementingAnOperation(kv *api.KV, deploymentID, nodeName, operationName, requirementIndex string) (string, error) {
	relTypeInit, err := GetRelationshipForRequirement(kv, deploymentID, nodeName, requirementIndex)
	if err != nil {
		return "", err
	}
	relType := relTypeInit
	for relType != "" {
		operationPath := getOperationPath(deploymentID, relType, operationName)
		kvp, _, err := kv.Get(path.Join(operationPath, "name"), nil)
		if err != nil {
			return "", errors.Wrap(err, consulutil.ConsulGenericErrMsg)
		}
		if kvp != nil && len(kvp.Value) > 0 {
			return relType, nil
		}
		relType, err = GetParentType(kv, deploymentID, relType)
	}
	return "", operationNotImplemented{msg: fmt.Sprintf("Operation %q not found in the type hierarchy of relationship %q", operationName, relTypeInit)}
}

// GetNodeTypeImplementingAnOperation returns the first (bottom-up) type in the type hierarchy of a given node that implements a given operation
//
// This is a shortcut for retrieving the node type and calling the GetTypeImplementingAnOperation() function
func GetNodeTypeImplementingAnOperation(kv *api.KV, deploymentID, nodeName, operationName string) (string, error) {
	nodeType, err := GetNodeType(kv, deploymentID, nodeName)
	if err != nil {
		return "", err
	}
	t, err := GetTypeImplementingAnOperation(kv, deploymentID, nodeType, operationName)
	return t, errors.Wrapf(err, "operation not found for node %q", nodeName)
}

// GetTypeImplementingAnOperation returns the first (bottom-up) type in the type hierarchy that implements a given operation
//
// An error is returned if the operation is not found in the type hierarchy
func GetTypeImplementingAnOperation(kv *api.KV, deploymentID, typeName, operationName string) (string, error) {
	implType := typeName
	for implType != "" {
		operationPath := getOperationPath(deploymentID, implType, operationName)
		kvp, _, err := kv.Get(path.Join(operationPath, "name"), nil)
		if err != nil {
			return "", errors.Wrap(err, consulutil.ConsulGenericErrMsg)
		}
		if kvp != nil && len(kvp.Value) > 0 {
			return implType, nil
		}
		implType, err = GetParentType(kv, deploymentID, implType)
	}
	return "", operationNotImplemented{msg: fmt.Sprintf("operation %q not found in the type hierarchy of type %q", operationName, typeName)}
}

// GetOperationImplementationType allows you when the implementation of an operation is an artifact to retrieve the type of this artifact
func GetOperationImplementationType(kv *api.KV, deploymentID, nodeType, operationName string) (string, error) {
	operationPath := getOperationPath(deploymentID, nodeType, operationName)
	kvp, _, err := kv.Get(path.Join(operationPath, "implementation/type"), nil)
	if err != nil {
		return "", errors.Wrap(err, "Fail to get the type of operation implementation")
	}

	if kvp == nil {
		return "", errors.Errorf("Operation type not found for %q", operationName)
	}

	return string(kvp.Value), nil
}

// GetOperationImplementationFile allows you when the implementation of an operation is an artifact to retrieve the file of this artifact
func GetOperationImplementationFile(kv *api.KV, deploymentID, nodeType, operationName string) (string, error) {
	operationPath := getOperationPath(deploymentID, nodeType, operationName)
	kvp, _, err := kv.Get(path.Join(operationPath, "implementation/file"), nil)
	if err != nil {
		return "", errors.Wrap(err, "Fail to get the file of operation implementation")
	}

	if kvp == nil {
		return "", errors.Errorf("Operation type not found for %q", operationName)
	}

	return string(kvp.Value), nil
}

// GetOperationImplementationRepository allows you when the implementation of an operation is an artifact to retrieve the repository of this artifact
func GetOperationImplementationRepository(kv *api.KV, deploymentID, nodeType, operationName string) (string, error) {
	operationPath := getOperationPath(deploymentID, nodeType, operationName)
	kvp, _, err := kv.Get(path.Join(operationPath, "implementation/repository"), nil)
	if err != nil {
		return "", errors.Wrap(err, "Fail to get the file of operation implementation")
	}

	if kvp == nil {
		return "", errors.Errorf("Operation type not found for %q", operationName)
	}

	return string(kvp.Value), nil
}

// IsNormativeOperation checks if a given operationName is known as a normative operation.
//
// The given operationName should be the fully qualified operation name composed of the <interface_type_name>.<operation_name>
// Basically this function checks if operationName starts with either tosca.interfaces.node.lifecycle.Standard or tosca.interfaces.relationship.Configure (the case is ignored)
func IsNormativeOperation(kv *api.KV, deploymentID, operationName string) bool {
	operationName = strings.ToLower(operationName)
	return strings.HasPrefix(operationName, "tosca.interfaces.relationship.configure") || strings.HasPrefix(operationName, "tosca.interfaces.node.lifecycle.standard")
}

// IsRelationshipOperationOnTargetNode returns true if the given operationName contains one of the following patterns (case doesn't matter):
//		pre_configure_target, post_configure_target, add_source
// Those patterns indicates that a relationship operation executes on the target node
func IsRelationshipOperationOnTargetNode(operationName string) bool {
	op := strings.ToLower(operationName)
	if strings.Contains(op, "pre_configure_target") || strings.Contains(op, "post_configure_target") || strings.Contains(op, "add_source") {
		return true
	}
	return false
}

// DecodeOperation takes a given operationName that should be formated as <fully_qualified_operation_name> or <fully_qualified_relationship_operation_name>/<requirementIndex> or <fully_qualified_relationship_operation_name>/<requirementName>/<targetNodeName>
// and extract the revelant information
//
// * isRelationshipOp indicates if operationName follows one of the relationship operation format
// * operationRealName extracts the fully_qualified_operation_name (identical to operationName if isRelationshipOp==false)
// * requirementIndex is the index of the requirement for this relationship operation (empty if isRelationshipOp==false)
// * targetNodeName is the name of the target node for this relationship operation (empty if isRelationshipOp==false)
func DecodeOperation(kv *api.KV, deploymentID, nodeName, operationName string) (isRelationshipOp bool, operationRealName, requirementIndex, targetNodeName string, err error) {
	opParts := strings.Split(operationName, "/")
	if len(opParts) == 1 {
		// not a relationship use default for return values
		operationRealName = operationName
		return
	} else if len(opParts) == 2 {
		isRelationshipOp = true
		operationRealName = opParts[0]
		requirementIndex = opParts[1]

		targetNodeName, err = GetTargetNodeForRequirement(kv, deploymentID, nodeName, requirementIndex)
		return
	} else if len(opParts) == 3 {
		isRelationshipOp = true
		operationRealName = opParts[0]
		requirementName := opParts[1]
		targetNodeName = opParts[2]
		var requirementPath string
		requirementPath, err = GetRequirementByNameAndTargetForNode(kv, deploymentID, nodeName, requirementName, targetNodeName)
		if err != nil {
			return
		}
		if requirementPath == "" {
			err = errors.Errorf("Unable to find a matching requirement for this relationship operation %q, source node %q, requirement name %q, target node %q", operationName, nodeName, requirementName, targetNodeName)
			return
		}
		requirementIndex = path.Base(requirementPath)
		return
	}
	err = errors.Errorf("operation %q doesn't follow the format <fully_qualified_operation_name>/<requirementIndex> or <fully_qualified_operation_name>/<requirementName>/<targetNodeName>", operationName)
	return
}

// GetOperationOutputForNode return a map with in index the instance number and in value the result of the output
// The "params" parameter is necessary to pass the path of the output
func GetOperationOutputForNode(kv *api.KV, deploymentID, nodeName, instanceName, interfaceName, operationName, outputName string) (string, error) {
	instancesPath := path.Join(consulutil.DeploymentKVPrefix, deploymentID, "topology/instances", nodeName)

	output, _, err := kv.Get(filepath.Join(instancesPath, instanceName, "outputs", strings.ToLower(interfaceName), strings.ToLower(operationName), outputName), nil)
	if err != nil {
		return "", errors.Wrap(err, consulutil.ConsulGenericErrMsg)
	}
	if output != nil && len(output.Value) > 0 {
		return string(output.Value), nil
	}
	// Look at host node
	var host string
	host, err = GetHostedOnNode(kv, deploymentID, nodeName)
	if err != nil {
		return "", err
	}
	if host != "" {
		// TODO we consider that instance name is the same for the host but we should not
		return GetOperationOutputForNode(kv, deploymentID, host, instanceName, interfaceName, operationName, outputName)
	}
	return "", nil
}

// GetOperationOutputForRelationship retrieves an operation output for a relationship
// The returned value may be empty if the operation output could not be retrieved
func GetOperationOutputForRelationship(kv *api.KV, deploymentID, nodeName, instanceName, requirementIndex, interfaceName, operationName, outputName string) (string, error) {
	result, _, err := kv.Get(path.Join(consulutil.DeploymentKVPrefix, deploymentID, "topology/relationship_instances", nodeName, requirementIndex, instanceName, "outputs", strings.ToLower(path.Join(interfaceName, operationName)), outputName), nil)
	if err != nil {
		return "", err
	}

	if result == nil || len(result.Value) == 0 {
		return "", nil
	}
	return string(result.Value), nil
}

func getOperationOutputForRequirements(kv *api.KV, deploymentID, nodeName, instanceName, interfaceName, operationName, outputName string) (string, error) {
	reqIndexes, err := GetRequirementsIndexes(kv, deploymentID, nodeName)
	if err != nil {
		return "", err
	}
	for _, reqIndex := range reqIndexes {
		result, err := GetOperationOutputForRelationship(kv, deploymentID, nodeName, instanceName, reqIndex, interfaceName, operationName, outputName)
		if err != nil || result != "" {
			return result, err
		}
	}
	return "", nil
}

// GetImplementationArtifactForExtension returns the implementation artifact type for a given extension.
//
// If the extension is unknown then an empty string is returned
func GetImplementationArtifactForExtension(kv *api.KV, deploymentID, extension string) (string, error) {
	extension = strings.ToLower(extension)
	kvp, _, err := kv.Get(path.Join(consulutil.DeploymentKVPrefix, deploymentID, "topology", implementationArtifactsExtensionsPath, extension), nil)
	if err != nil {
		return "", errors.Wrap(err, consulutil.ConsulGenericErrMsg)
	}
	if kvp == nil {
		return "", nil
	}
	return string(kvp.Value), nil
}

// GetImplementationArtifactForOperation returns the implementation artifact type for a given operation.
// operationName, isRelationshipOp and requirementIndex are typically the result of the DecodeOperation function that
// should generally call prior to call this function.
func GetImplementationArtifactForOperation(kv *api.KV, deploymentID, nodeName, operationName string, isRelationshipOp bool, requirementIndex string) (string, error) {
	var nodeOrRelType string
	var err error
	if isRelationshipOp {
		nodeOrRelType, err = GetRelationshipForRequirement(kv, deploymentID, nodeName, requirementIndex)
	} else {
		nodeOrRelType, err = GetNodeType(kv, deploymentID, nodeName)
	}
	if err != nil {
		return "", err
	}

	// TODO keep in mind that with Alien we may have a an implementation artifact directly in the operation. This part is currently under development in the Kubernetes branch
	// and we should take this into account when it will be merged.
	_, primary, err := GetOperationPathAndPrimaryImplementationForNodeType(kv, deploymentID, nodeOrRelType, operationName)
	if err != nil {
		return "", err
	}
	if primary == "" {
		implType, err := GetOperationImplementationType(kv, deploymentID, nodeOrRelType, operationName)
		if err != nil {
			return "", err
		}
		return implType, nil
	}
	primarySlice := strings.Split(primary, ".")
	ext := primarySlice[len(primarySlice)-1]
	artImpl, err := GetImplementationArtifactForExtension(kv, deploymentID, ext)
	if err != nil {
		return "", err
	}
	if artImpl == "" {
		return "", errors.Errorf("Failed to resolve implementation artifact for type %q, operation %q, implementation %q and extension %q", nodeOrRelType, operationName, primary, ext)
	}
	return artImpl, nil
}

// GetOperationInputs returns the list of inputs names for a given operation
func GetOperationInputs(kv *api.KV, deploymentID, typeName, operationName string) ([]string, error) {
	operationPath := getOperationPath(deploymentID, typeName, operationName)

	inputKeys, _, err := kv.Keys(operationPath+"/inputs/", "/", nil)
	if err != nil {
		return nil, errors.Wrap(err, consulutil.ConsulGenericErrMsg)
	}
	inputs := make([]string, len(inputKeys))
	for i, input := range inputKeys {
		inputs[i] = path.Base(input)
	}
	return inputs, nil
}

func getParentOperation(kv *api.KV, deploymentID string, operation prov.Operation) (prov.Operation, error) {
	parentType, err := GetParentType(kv, deploymentID, operation.ImplementedInType)
	if err != nil {
		return prov.Operation{}, err
	}
	if parentType != "" {
		opImplType, err := GetTypeImplementingAnOperation(kv, deploymentID, parentType, operation.Name)
		if err != nil {
			return prov.Operation{}, err
		}
		return prov.Operation{
			Name: operation.Name, ImplementationArtifact: operation.ImplementationArtifact,
			ImplementedInType: opImplType,
			RelOp: prov.RelationshipOperation{
				IsRelationshipOperation: operation.RelOp.IsRelationshipOperation,
				RequirementIndex:        operation.RelOp.RequirementIndex,
				TargetNodeName:          operation.RelOp.TargetNodeName,
			},
		}, nil
	}
	return prov.Operation{}, operationNotImplemented{msg: fmt.Sprintf("operation %q not found in the type hierarchy of type %q", operation.Name, operation.ImplementedInType)}

}

// An OperationInputResult represents a result of retrieving an operation input
//
// As in case of attributes it may have different values based on the instance name this struct contains the necessary information to identify the result context
type OperationInputResult struct {
	NodeName     string
	InstanceName string
	Value        string
}

// GetOperationInput retrieves the value of an input for a given operation
func GetOperationInput(kv *api.KV, deploymentID, nodeName string, operation prov.Operation, inputName string) ([]OperationInputResult, error) {
	isPropDef, err := IsOperationInputAPropertyDefinition(kv, deploymentID, operation.ImplementedInType, operation.Name, inputName)
	if err != nil {
		return nil, err
	} else if isPropDef {
		return nil, errors.Errorf("Input %q for operation %v is a property definition we can't resolve it without a task input", inputName, operation)
	}

	operationPath := getOperationPath(deploymentID, operation.ImplementedInType, operation.Name)
	inputPath := path.Join(operationPath, "inputs", inputName, "data")
	found, res, isFunction, err := getValueAssignmentWithoutResolve(kv, deploymentID, inputPath)
	if err != nil {
		return nil, err
	}
	results := make([]OperationInputResult, 0)
	if found {
		if !isFunction {
			instances, err := GetNodeInstancesIds(kv, deploymentID, nodeName)
			if err != nil {
				return nil, err
			}
			for _, ins := range instances {
				results = append(results, OperationInputResult{nodeName, ins, res})
			}
			return results, nil
		}
		va := &tosca.ValueAssignment{}
		err = yaml.Unmarshal([]byte(res), va)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal TOSCA Function definition %q", res)
		}
		f := va.GetFunction()
		var hasAttrOnTarget bool
		var hasAttrOnSrcOrSelf bool
		for _, ga := range f.GetFunctionsByOperator(tosca.GetAttributeOperator) {
			switch ga.Operands[0].String() {
			case funcKeywordTARGET:
				hasAttrOnTarget = true
			case funcKeywordSELF, funcKeywordSOURCE, funcKeywordHOST:
				hasAttrOnSrcOrSelf = true
			}
		}
		if hasAttrOnSrcOrSelf && hasAttrOnTarget {
			return nil, errors.Errorf("can't resolve input %q for operation %v on node %q: get_attribute functions on TARGET and SELF/SOURCE/HOST at the same time is not supported.", inputName, operation, nodeName)
		}
		var instances []string
		var ctxNodeName string
		if hasAttrOnTarget && operation.RelOp.IsRelationshipOperation {
			instances, err = GetNodeInstancesIds(kv, deploymentID, operation.RelOp.TargetNodeName)
			ctxNodeName = operation.RelOp.TargetNodeName
		} else {
			instances, err = GetNodeInstancesIds(kv, deploymentID, nodeName)
			ctxNodeName = nodeName
		}
		if err != nil {
			return nil, err
		}
		for _, ins := range instances {
			res, err = resolver(kv, deploymentID).context(withNodeName(nodeName), withInstanceName(ins), withRequirementIndex(operation.RelOp.RequirementIndex)).resolveFunction(f)
			if err != nil {
				return nil, err
			}
			results = append(results, OperationInputResult{ctxNodeName, ins, res})
		}
		return results, nil

	}
	// Check if it is implemented elsewhere
	newOp, err := getParentOperation(kv, deploymentID, operation)
	if err != nil {
		if !IsOperationNotImplemented(err) {
			return nil, err
		}
		return nil, inputNotFound{inputName, operation.Name, operation.ImplementedInType}
	}

	results, err = GetOperationInput(kv, deploymentID, nodeName, newOp, inputName)
	if err != nil && IsInputNotFound(err) {
		return nil, errors.Wrapf(err, "input not found in type %q", operation.ImplementedInType)
	}
	return results, err
}

// GetOperationInputPropertyDefinitionDefault retrieves the default value of an input of type property definition for a given operation
func GetOperationInputPropertyDefinitionDefault(kv *api.KV, deploymentID, nodeName string, operation prov.Operation, inputName string) ([]OperationInputResult, error) {
	isPropDef, err := IsOperationInputAPropertyDefinition(kv, deploymentID, operation.ImplementedInType, operation.Name, inputName)
	if err != nil {
		return nil, err
	} else if !isPropDef {
		return nil, errors.Errorf("Input %q for operation %v is not a property definition we can't resolve its default value", inputName, operation)
	}
	operationPath := getOperationPath(deploymentID, operation.ImplementedInType, operation.Name)
	inputPath := path.Join(operationPath, "inputs", inputName, "default")
	found, res, isFunction, err := getValueAssignmentWithoutResolve(kv, deploymentID, inputPath)
	if err != nil {
		return nil, err
	}
	results := make([]OperationInputResult, 0)
	if found {
		if isFunction {
			return nil, errors.Errorf("can't resolve input %q for operation %v on node %q: TOSCA function are not supported for property definition defaults.", inputName, operation, nodeName)
		}
		instances, err := GetNodeInstancesIds(kv, deploymentID, nodeName)
		if err != nil {
			return nil, err
		}
		for _, ins := range instances {
			results = append(results, OperationInputResult{nodeName, ins, res})
		}
		return results, nil
	}
	// Check if it is implemented elsewhere
	newOp, err := getParentOperation(kv, deploymentID, operation)
	if err != nil {
		if !IsOperationNotImplemented(err) {
			return nil, err
		}
		return nil, inputNotFound{inputName, operation.Name, operation.ImplementedInType}
	}

	results, err = GetOperationInputPropertyDefinitionDefault(kv, deploymentID, nodeName, newOp, inputName)
	if err != nil && IsInputNotFound(err) {
		return nil, errors.Wrapf(err, "input not found in type %q", operation.ImplementedInType)
	}
	return results, err
}

// IsOperationInputAPropertyDefinition checks if a given operation input is a property definition
func IsOperationInputAPropertyDefinition(kv *api.KV, deploymentID, typeName, operationName, inputName string) (bool, error) {
	operationPath := getOperationPath(deploymentID, typeName, operationName)
	kvp, _, err := kv.Get(path.Join(operationPath, "inputs", inputName, "is_property_definition"), nil)
	if err != nil {
		return false, errors.Wrap(err, consulutil.ConsulGenericErrMsg)
	}

	if kvp == nil || len(kvp.Value) == 0 {
		return false, errors.Errorf("Operation %q not found for type %q", operationName, typeName)
	}

	isPropDef, err := strconv.ParseBool(string(kvp.Value))
	return isPropDef, errors.Wrapf(err, "Failed to parse boolean for operation %q of type %q", operationName, typeName)
}
