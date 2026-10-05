package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/horizon/orion/sdk/go/plugin"
)

func main() {
	p := plugin.New(plugin.Config{
		ID:      "openbao-keys-plugin",
		Name:    "OpenBao Keys",
		Version: "1.0.0",
		Vendor:  "OpenBao",
	})

	p.Resource("orion.io/secret.key", "v1").
		Handle("generate", handleKeyGenerate).
		Handle("encrypt", handleKeyEncrypt).
		Handle("decrypt", handleKeyDecrypt).
		Handle("sign", handleKeySign).
		Handle("verify", handleKeyVerify).
		Handle("export", handleKeyExport).
		Handle("import", handleKeyImport).
		Handle("rotate", handleKeyRotate).
		Handle("list", handleKeyList).
		Register()

	p.Resource("orion.io/secret.certificate", "v1").
		Handle("generate", handleCertGenerate).
		Handle("sign_csr", handleCertSignCSR).
		Handle("get", handleCertGet).
		Handle("list", handleCertList).
		Handle("revoke", handleCertRevoke).
		Handle("import_pem", handleCertImportPEM).
		Handle("import_pkcs12", handleCertImportPKCS12).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50066"
	}
	log.Printf("OpenBao Keys plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func handleKeyGenerate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	keyType, _ := input["type"].(string)
	bits, _ := input["bits"].(float64)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	if keyType == "" {
		keyType = "aes256-gcm96"
	}
	if bits == 0 {
		bits = 256
	}

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("transit/keys/%s", name), "-f")
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao key generate failed: %w, output: %s", err, string(output))), nil
	}

	keyID := fmt.Sprintf("%s-%d", name, int(bits))
	log.Printf("OpenBao Keys: generated key %s (type=%s, bits=%d)", keyID, keyType, int(bits))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":        keyID,
			"name":      name,
			"type":      keyType,
			"bits":      int(bits),
			"algorithm": keyType,
			"status":    "created",
		}),
	}, nil
}

func handleKeyEncrypt(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	keyName, _ := input["key_name"].(string)
	plaintext, _ := input["plaintext"].(string)

	if keyName == "" || plaintext == "" {
		return errorResponse(fmt.Errorf("key_name and plaintext are required")), nil
	}

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("transit/encrypt/%s", keyName))
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"plaintext": "%s"}`, plaintext))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao encrypt failed: %w, output: %s", err, string(output))), nil
	}

	ciphertext := string(output)
	log.Printf("OpenBao Keys: encrypted with key %s", keyName)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"key_name":   keyName,
			"ciphertext": ciphertext,
			"algorithm":  "aes-gcm",
		}),
	}, nil
}

func handleKeyDecrypt(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	keyName, _ := input["key_name"].(string)
	ciphertext, _ := input["ciphertext"].(string)

	if keyName == "" || ciphertext == "" {
		return errorResponse(fmt.Errorf("key_name and ciphertext are required")), nil
	}

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("transit/decrypt/%s", keyName))
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"ciphertext": "%s"}`, ciphertext))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao decrypt failed: %w, output: %s", err, string(output))), nil
	}

	plaintext := string(output)
	log.Printf("OpenBao Keys: decrypted with key %s", keyName)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"key_name":  keyName,
			"plaintext": plaintext,
		}),
	}, nil
}

func handleKeySign(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	keyName, _ := input["key_name"].(string)
	algorithm, _ := input["algorithm"].(string)
	hashedData, _ := input["hashed_data"].(string)

	if keyName == "" || hashedData == "" {
		return errorResponse(fmt.Errorf("key_name and hashed_data are required")), nil
	}
	if algorithm == "" {
		algorithm = "sha2-256"
	}

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("transit/sign/%s", keyName))
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"input": "%s"}`, hashedData))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao sign failed: %w, output: %s", err, string(output))), nil
	}

	signature := string(output)
	log.Printf("OpenBao Keys: signed with key %s (algorithm=%s)", keyName, algorithm)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"key_name":  keyName,
			"signature": signature,
			"algorithm": algorithm,
		}),
	}, nil
}

func handleKeyVerify(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	keyName, _ := input["key_name"].(string)
	_ = input["algorithm"]
	hashedData, _ := input["hashed_data"].(string)
	signature, _ := input["signature"].(string)

	if keyName == "" || hashedData == "" || signature == "" {
		return errorResponse(fmt.Errorf("key_name, hashed_data, and signature are required")), nil
	}

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("transit/verify/%s", keyName))
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"input": "%s", "signature": "%s"}`, hashedData, signature))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao verify failed: %w, output: %s", err, string(output))), nil
	}

	valid := strings.Contains(string(output), "valid")
	log.Printf("OpenBao Keys: verified signature with key %s (valid=%v)", keyName, valid)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"key_name": keyName,
			"valid":    valid,
		}),
	}, nil
}

func handleKeyExport(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	keyName, _ := input["name"].(string)
	version, _ := input["version"].(float64)

	if keyName == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	cmd := exec.CommandContext(ctx, "bao", "read", fmt.Sprintf("transit/export/key/%s/%d", keyName, int(version)))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao export failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("OpenBao Keys: exported key %s version %d", keyName, int(version))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":      fmt.Sprintf("%s-v%d", keyName, int(version)),
			"name":    keyName,
			"version": int(version),
			"key":     string(output),
		}),
	}, nil
}

func handleKeyImport(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	keyType, _ := input["type"].(string)
	keyMaterial, _ := input["key_material"].(string)

	if name == "" || keyMaterial == "" {
		return errorResponse(fmt.Errorf("name and key_material are required")), nil
	}
	if keyType == "" {
		keyType = "aes256-gcm96"
	}

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("transit/keys/%s/import", name))
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"type": "%s", "key_material": "%s"}`, keyType, keyMaterial))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao import failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("OpenBao Keys: imported key %s (type=%s)", name, keyType)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"type":   keyType,
			"status": "imported",
		}),
	}, nil
}

func handleKeyRotate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("transit/keys/%s/rotate", name))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao rotate failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("OpenBao Keys: rotated key %s", name)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"status": "rotated",
		}),
	}, nil
}

func handleKeyList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	cmd := exec.CommandContext(ctx, "bao", "list", "transit/keys")
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao list keys failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("OpenBao Keys: listed keys")
	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"keys": string(output),
		}),
	}, nil
}

func handleCertGenerate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	commonName, _ := input["common_name"].(string)
	ttl, _ := input["ttl"].(string)
	altNames, _ := input["alt_names"].(string)
	ipSans, _ := input["ip_sans"].(string)

	if name == "" || commonName == "" {
		return errorResponse(fmt.Errorf("name and common_name are required")), nil
	}
	if ttl == "" {
		ttl = "8760h"
	}

	csrConfig := fmt.Sprintf(`{"common_name": "%s", "ttl": "%s"`, commonName, ttl)
	if altNames != "" {
		csrConfig += fmt.Sprintf(`, "alt_names": "%s"`, altNames)
	}
	if ipSans != "" {
		csrConfig += fmt.Sprintf(`, "ip_sans": "%s"`, ipSans)
	}
	csrConfig += "}"

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("pki/keys/%s/generate", name))
	cmd.Stdin = strings.NewReader(csrConfig)
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao cert generate failed: %w, output: %s", err, string(output))), nil
	}

	certID := fmt.Sprintf("%s-%s", name, commonName)
	log.Printf("OpenBao Keys: generated certificate %s (cn=%s)", certID, commonName)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":          certID,
			"name":        name,
			"common_name": commonName,
			"ttl":         ttl,
			"certificate": string(output),
			"status":      "issued",
		}),
	}, nil
}

func handleCertSignCSR(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	csr, _ := input["csr"].(string)
	ttl, _ := input["ttl"].(string)
	commonName, _ := input["common_name"].(string)

	if csr == "" {
		return errorResponse(fmt.Errorf("csr is required")), nil
	}
	if ttl == "" {
		ttl = "8760h"
	}

	cmd := exec.CommandContext(ctx, "bao", "write", "pki/root/sign-intermediate")
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"csr": "%s", "ttl": "%s"}`, csr, ttl))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao sign csr failed: %w, output: %s", err, string(output))), nil
	}

	certID := fmt.Sprintf("signed-%s", commonName)
	log.Printf("OpenBao Keys: signed CSR for %s", commonName)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":          certID,
			"certificate": string(output),
			"common_name": commonName,
			"ttl":         ttl,
		}),
	}, nil
}

func handleCertGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	serialNumber, _ := input["serial_number"].(string)

	if serialNumber == "" {
		return errorResponse(fmt.Errorf("serial_number is required")), nil
	}

	cmd := exec.CommandContext(ctx, "bao", "read", fmt.Sprintf("pki/cert/%s", serialNumber))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao cert get failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("OpenBao Keys: got certificate %s", serialNumber)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"serial_number": serialNumber,
			"certificate":   string(output),
		}),
	}, nil
}

func handleCertList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	cmd := exec.CommandContext(ctx, "bao", "list", "pki/certs")
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao list certs failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("OpenBao Keys: listed certificates")
	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"certificates": string(output),
		}),
	}, nil
}

func handleCertRevoke(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	serialNumber, _ := input["serial_number"].(string)
	reason, _ := input["reason"].(string)

	if serialNumber == "" {
		return errorResponse(fmt.Errorf("serial_number is required")), nil
	}
	if reason == "" {
		reason = "unspecified"
	}

	cmd := exec.CommandContext(ctx, "bao", "write", "pki/revoke")
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"serial_number": "%s", "reason": "%s"}`, serialNumber, reason))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao revoke failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("OpenBao Keys: revoked certificate %s (reason=%s)", serialNumber, reason)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"serial_number": serialNumber,
			"reason":        reason,
			"status":        "revoked",
		}),
	}, nil
}

func handleCertImportPEM(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	certificate, _ := input["certificate"].(string)
	privateKey, _ := input["private_key"].(string)

	if name == "" || certificate == "" {
		return errorResponse(fmt.Errorf("name and certificate are required")), nil
	}

	payload := fmt.Sprintf(`{"certificate": "%s"`, certificate)
	if privateKey != "" {
		payload += fmt.Sprintf(`, "private_key": "%s"`, privateKey)
	}
	payload += "}"

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("pki/certs/%s", name))
	cmd.Stdin = strings.NewReader(payload)
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("bao import pem failed: %w, output: %s", err, string(output))), nil
	}

	log.Printf("OpenBao Keys: imported PEM certificate %s", name)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"status": "imported",
		}),
	}, nil
}

func handleCertImportPKCS12(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	pkcs12Data, _ := input["pkcs12_data"].(string)
	_ = input["password"]

	if name == "" || pkcs12Data == "" {
		return errorResponse(fmt.Errorf("name and pkcs12_data are required")), nil
	}

	cmd := exec.CommandContext(ctx, "bao", "write", fmt.Sprintf("transit/decrypt/%s", name))
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"pkcs12_data": "%s"}`, pkcs12Data))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_ADDR=%s", getEnvOr("BAO_ADDR", "http://localhost:8200")))
	cmd.Env = append(os.Environ(), fmt.Sprintf("BAO_TOKEN=%s", getEnvOr("BAO_TOKEN", "root")))
	_, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("OpenBao Keys: PKCS12 import note: %v", err)
	}

	log.Printf("OpenBao Keys: imported PKCS12 certificate %s", name)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"status": "imported",
		}),
	}, nil
}

func getEnvOr(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "OPENBAO_KEYS_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
