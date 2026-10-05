package schema

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

type Validator struct {
	schemas    map[string]*jsonschema.Schema
	compiler   *jsonschema.Compiler
}

func NewValidator() *Validator {
	return &Validator{
		schemas:  make(map[string]*jsonschema.Schema),
		compiler: jsonschema.NewCompiler(),
	}
}

func (v *Validator) LoadSchema(name string, rawJSON []byte) error {
	if err := v.compiler.AddResource(name, strings.NewReader(string(rawJSON))); err != nil {
		return fmt.Errorf("failed to load schema %s: %w", name, err)
	}

	schema, err := v.compiler.Compile(name)
	if err != nil {
		return fmt.Errorf("failed to compile schema %s: %w", name, err)
	}

	v.schemas[name] = schema
	return nil
}

func (v *Validator) LoadSchemaFromYAML(name string, rawYAML []byte) error {
	var jsonData interface{}
	if err := yaml.Unmarshal(rawYAML, &jsonData); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	jsonBytes, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to convert YAML to JSON: %w", err)
	}

	return v.LoadSchema(name, jsonBytes)
}

func (v *Validator) Validate(name string, data interface{}) error {
	schema, ok := v.schemas[name]
	if !ok {
		return fmt.Errorf("schema not found: %s", name)
	}

	if err := schema.Validate(data); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}

func (v *Validator) ValidateJSON(name string, rawJSON []byte) error {
	var data interface{}
	if err := json.Unmarshal(rawJSON, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	return v.Validate(name, data)
}

type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("%s: %s", e.Path, e.Message)
	}
	return e.Message
}

type PathValidator struct {
	AllowedParams map[string]*regexp.Regexp
	RequiredParams []string
}

func NewPathValidator() *PathValidator {
	return &PathValidator{
		AllowedParams: make(map[string]*regexp.Regexp),
	}
}

func (v *PathValidator) AddParam(name string, pattern string) {
	if pattern != "" {
		v.AllowedParams[name] = regexp.MustCompile(pattern)
	} else {
		v.AllowedParams[name] = nil
	}
}

func (v *PathValidator) AddRequired(params ...string) {
	v.RequiredParams = append(v.RequiredParams, params...)
}

func (v *PathValidator) Validate(params map[string]string) []ValidationError {
	var errors []ValidationError

	for _, required := range v.RequiredParams {
		if val, ok := params[required]; !ok || val == "" {
			errors = append(errors, ValidationError{
				Path:    "path",
				Message: fmt.Sprintf("required parameter missing: %s", required),
			})
		}
	}

	for name, pattern := range v.AllowedParams {
		if val, ok := params[name]; ok {
			if pattern != nil && !pattern.MatchString(val) {
				errors = append(errors, ValidationError{
					Path:    "path." + name,
					Message: fmt.Sprintf("parameter %s has invalid format: %s", name, val),
				})
			}
		}
	}

	return errors
}

type BodyValidator struct {
	disallowUnknownFields bool
	allowedFields        map[string]struct{}
	requiredFields       []string
	fieldSchemas        map[string]*jsonschema.Schema
}

func NewBodyValidator() *BodyValidator {
	return &BodyValidator{
		allowedFields:  make(map[string]struct{}),
		fieldSchemas:  make(map[string]*jsonschema.Schema),
	}
}

func (v *BodyValidator) SetDisallowUnknownFields(b bool) {
	v.disallowUnknownFields = b
}

func (v *BodyValidator) AllowFields(fields ...string) {
	for _, f := range fields {
		v.allowedFields[f] = struct{}{}
	}
}

func (v *BodyValidator) RequireFields(fields ...string) {
	v.requiredFields = append(v.requiredFields, fields...)
}

func (v *BodyValidator) AddFieldSchema(field string, schema *jsonschema.Schema) {
	v.fieldSchemas[field] = schema
}

func (v *BodyValidator) ValidateBody(rawJSON []byte) ([]ValidationError, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(rawJSON, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if data == nil {
		return nil, fmt.Errorf("invalid JSON: expected object")
	}

	var errors []ValidationError

	for _, required := range v.requiredFields {
		if _, ok := data[required]; !ok {
			errors = append(errors, ValidationError{
				Path:    "body",
				Message: fmt.Sprintf("required field missing: %s", required),
			})
		}
	}

	if v.disallowUnknownFields {
		known := make(map[string]struct{}, len(v.allowedFields)+len(v.requiredFields)+len(v.fieldSchemas))
		for field := range v.allowedFields {
			known[field] = struct{}{}
		}
		for _, field := range v.requiredFields {
			known[field] = struct{}{}
		}
		for field := range v.fieldSchemas {
			known[field] = struct{}{}
		}
		for key := range data {
			if _, allowed := known[key]; !allowed {
				errors = append(errors, ValidationError{
					Path:    "body." + key,
					Message: "field not allowed",
				})
			}
		}
	} else if len(v.allowedFields) > 0 {
		for key := range data {
			if _, allowed := v.allowedFields[key]; !allowed {
				errors = append(errors, ValidationError{Path: "body." + key, Message: "field not allowed"})
			}
		}
	}

	for field, schema := range v.fieldSchemas {
		if val, ok := data[field]; ok {
			if err := schema.Validate(val); err != nil {
				errors = append(errors, ValidationError{
					Path:    "body." + field,
					Message: err.Error(),
				})
			}
		}
	}

	return errors, nil
}

func ValidateURLPath(path string, pattern string) error {
	if strings.ContainsAny(path, "\x00\r\n") || strings.Contains(path, "..") {
		return fmt.Errorf("unsafe path")
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return fmt.Errorf("invalid escaped path: %w", err)
	}
	if strings.ContainsAny(decoded, "\\/") || strings.Contains(decoded, "..") {
		return fmt.Errorf("unsafe path")
	}
	matched, err := regexp.MatchString(pattern, path)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}
	if !matched {
		return fmt.Errorf("path %s does not match pattern %s", path, pattern)
	}
	return nil
}

func ValidateURLParams(rawURL string, expectedParams []string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	query := u.Query()
	for _, param := range expectedParams {
		if _, ok := query[param]; !ok {
			return fmt.Errorf("required query parameter missing: %s", param)
		}
	}

	return nil
}

func MustBeJSONObject(rawJSON []byte) (map[string]interface{}, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(rawJSON, &obj); err != nil {
		return nil, fmt.Errorf("expected JSON object: %w", err)
	}
	return obj, nil
}

func MustBeJSONArray(rawJSON []byte) ([]interface{}, error) {
	var arr []interface{}
	if err := json.Unmarshal(rawJSON, &arr); err != nil {
		return nil, fmt.Errorf("expected JSON array: %w", err)
	}
	return arr, nil
}

func ExtractField(data interface{}, path string) (interface{}, error) {
	parts := strings.Split(path, ".")
	var current interface{} = data

	for _, part := range parts {
		switch c := current.(type) {
		case map[string]interface{}:
			val, ok := c[part]
			if !ok {
				return nil, fmt.Errorf("field not found: %s", part)
			}
			current = val
		default:
			return nil, fmt.Errorf("cannot index into %T", current)
		}
	}

	return current, nil
}

func GetType(value interface{}) string {
	if value == nil {
		return "nil"
	}
	return reflect.TypeOf(value).String()
}
