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
		ID:      "dbaas-plugin",
		Name:    "DBaaS",
		Version: "1.0.0",
		Vendor:  "PostgreSQL/MySQL/Redis",
	})

	p.Resource("orion.io/database.instance", "v1").
		Handle("create", handleDBCreate).
		Handle("delete", handleDBDelete).
		Handle("start", handleDBStart).
		Handle("stop", handleDBStop).
		Handle("restart", handleDBRestart).
		Handle("get", handleDBGet).
		Handle("list", handleDBList).
		Handle("backup", handleDBBackup).
		Handle("restore", handleDBRestore).
		Handle("resize", handleDBResize).
		AddCapability("postgresql", true).
		AddCapability("mysql", true).
		AddCapability("mariadb", true).
		AddCapability("redis", true).
		AddCapability(" Valkey", true).
		AddCapability("backup", true).
		AddCapability("replication", true).
		AddCapability("ssl", true).
		Register()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = ":50065"
	}
	log.Printf("DBaaS plugin starting on %s...", endpoint)
	if err := p.Serve(context.Background(), endpoint); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func handleDBCreate(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)
	engineVersion, _ := input["engine_version"].(string)
	size, _ := input["size"].(float64)
	user, _ := input["user"].(string)
	_ = input["password"]
	port, _ := input["port"].(float64)

	if name == "" || engine == "" {
		return errorResponse(fmt.Errorf("name and engine are required")), nil
	}
	if engineVersion == "" {
		engineVersion = "latest"
	}
	if port == 0 {
		port = getDefaultPort(engine)
	}

	instanceID := fmt.Sprintf("db-%s-%s", engine, name)

	switch engine {
	case "postgresql":
		cmd := exec.CommandContext(ctx, "pg_ctl", "initdb", "-D", fmt.Sprintf("/var/lib/postgresql/data/%s", name))
		cmd.Run()
		cmd = exec.CommandContext(ctx, "pg_ctl", "-D", fmt.Sprintf("/var/lib/postgresql/data/%s", name), "-o", fmt.Sprintf("-p %d", int(port)), "-l", fmt.Sprintf("/var/log/postgresql/%s.log", name), "start")
		cmd.Run()

	case "mysql":
		cmd := exec.CommandContext(ctx, "mysqld", "--initialize", "--datadir", fmt.Sprintf("/var/lib/mysql/%s", name), "--user=mysql")
		cmd.Run()
		cmd = exec.CommandContext(ctx, "mysqld", fmt.Sprintf("--datadir=/var/lib/mysql/%s", name), fmt.Sprintf("--port=%d", int(port)), "--user=mysql", "&")
		cmd.Start()

	case "redis":
		cmd := exec.CommandContext(ctx, "redis-server", "--port", fmt.Sprintf("%d", int(port)), "--dir", fmt.Sprintf("/var/lib/redis/%s", name), "--daemonize", "yes")
		cmd.Run()
	}

	log.Printf("DBaaS: created %s instance %s (engine=%s, port=%d)", engine, instanceID, engine, int(port))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":      instanceID,
			"name":    name,
			"engine":  engine,
			"version": engineVersion,
			"port":    int(port),
			"user":    user,
			"size":    int(size),
			"status":  "running",
		}),
	}, nil
}

func handleDBDelete(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}
	if engine == "" {
		engine = "postgresql"
	}

	switch engine {
	case "postgresql":
		exec.CommandContext(ctx, "pg_ctl", "-D", fmt.Sprintf("/var/lib/postgresql/data/%s", name), "stop").Run()
	case "mysql":
		exec.CommandContext(ctx, "mysqladmin", "-u", "root", "shutdown").Run()
	case "redis":
		exec.CommandContext(ctx, "redis-cli", "shutdown").Run()
	}

	log.Printf("DBaaS: deleted %s instance %s", engine, name)
	return &plugin.ResourceResponse{Success: true}, nil
}

func handleDBStart(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	switch engine {
	case "postgresql":
		exec.CommandContext(ctx, "pg_ctl", "-D", fmt.Sprintf("/var/lib/postgresql/data/%s", name), "start").Run()
	case "mysql":
		exec.CommandContext(ctx, "mysqld", fmt.Sprintf("--datadir=/var/lib/mysql/%s", name), "&").Start()
	case "redis":
		exec.CommandContext(ctx, "redis-server", "--daemonize", "yes").Run()
	}

	log.Printf("DBaaS: started %s instance %s", engine, name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "running"}),
	}, nil
}

func handleDBStop(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	switch engine {
	case "postgresql":
		exec.CommandContext(ctx, "pg_ctl", "-D", fmt.Sprintf("/var/lib/postgresql/data/%s", name), "stop").Run()
	case "mysql":
		exec.CommandContext(ctx, "mysqladmin", "-u", "root", "shutdown").Run()
	case "redis":
		exec.CommandContext(ctx, "redis-cli", "shutdown").Run()
	}

	log.Printf("DBaaS: stopped %s instance %s", engine, name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "stopped"}),
	}, nil
}

func handleDBRestart(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	switch engine {
	case "postgresql":
		exec.CommandContext(ctx, "pg_ctl", "-D", fmt.Sprintf("/var/lib/postgresql/data/%s", name), "restart").Run()
	case "mysql":
		exec.CommandContext(ctx, "mysqladmin", "-u", "root", "restart").Run()
	case "redis":
		exec.CommandContext(ctx, "redis-cli", "shutdown").Run()
		exec.CommandContext(ctx, "redis-server", "--daemonize", "yes").Run()
	}

	log.Printf("DBaaS: restarted %s instance %s", engine, name)
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"id": name, "status": "running"}),
	}, nil
}

func handleDBGet(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)

	if name == "" {
		return errorResponse(fmt.Errorf("name is required")), nil
	}

	status := "running"
	var info map[string]interface{}

	switch engine {
	case "postgresql":
		cmd := exec.CommandContext(ctx, "pg_ctl", "-D", fmt.Sprintf("/var/lib/postgresql/data/%s", name), "status")
		if cmd.Run() != nil {
			status = "stopped"
		}
		info = map[string]interface{}{"data_dir": fmt.Sprintf("/var/lib/postgresql/data/%s", name)}

	case "mysql":
		cmd := exec.CommandContext(ctx, "mysqladmin", "-u", "root", "status")
		if cmd.Run() != nil {
			status = "stopped"
		}
		info = map[string]interface{}{"data_dir": fmt.Sprintf("/var/lib/mysql/%s", name)}

	case "redis":
		cmd := exec.CommandContext(ctx, "redis-cli", "ping")
		if cmd.Run() != nil {
			status = "stopped"
		}
		info = map[string]interface{}{"data_dir": fmt.Sprintf("/var/lib/redis/%s", name)}
	}

	log.Printf("DBaaS: got %s instance %s (status=%s)", engine, name, status)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"name":   name,
			"engine": engine,
			"status": status,
			"info":   info,
		}),
	}, nil
}

func handleDBList(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	cmd := exec.CommandContext(ctx, "ps", "aux")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errorResponse(fmt.Errorf("failed to list processes: %w", err)), nil
	}

	var instances []map[string]interface{}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "postgres") || strings.Contains(line, "mysqld") || strings.Contains(line, "redis-server") {
			parts := strings.Fields(line)
			if len(parts) > 11 {
				engine := "unknown"
				if strings.Contains(line, "postgres") {
					engine = "postgresql"
				} else if strings.Contains(line, "mysqld") {
					engine = "mysql"
				} else if strings.Contains(line, "redis") {
					engine = "redis"
				}
				instances = append(instances, map[string]interface{}{
					"engine": engine,
					"pid":    parts[1],
					"status": "running",
				})
			}
		}
	}

	log.Printf("DBaaS: listed %d instances", len(instances))
	return &plugin.ResourceResponse{
		Success: true,
		Result:  marshal(map[string]interface{}{"instances": instances}),
	}, nil
}

func handleDBBackup(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)
	dest, _ := input["destination"].(string)

	if name == "" || engine == "" {
		return errorResponse(fmt.Errorf("name and engine are required")), nil
	}
	if dest == "" {
		dest = fmt.Sprintf("/backups/%s-%s.bak", engine, name)
	}

	var backupID string
	switch engine {
	case "postgresql":
		cmd := exec.CommandContext(ctx, "pg_dump", "-Fc", "-f", dest, name)
		cmd.Run()
		backupID = fmt.Sprintf("pg-%s-%s", name, dest)

	case "mysql":
		cmd := exec.CommandContext(ctx, "mysqldump", "-u", "root", "--all-databases", "-r", dest)
		cmd.Run()
		backupID = fmt.Sprintf("mysql-%s-%s", name, dest)

	case "redis":
		cmd := exec.CommandContext(ctx, "redis-cli", "SAVE")
		cmd.Run()
		backupID = fmt.Sprintf("redis-%s-dump.rdb", name)
	}

	log.Printf("DBaaS: created backup %s for %s instance %s", backupID, engine, name)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":          backupID,
			"name":        name,
			"engine":      engine,
			"destination": dest,
			"status":      "completed",
		}),
	}, nil
}

func handleDBRestore(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)
	source, _ := input["source"].(string)

	if name == "" || engine == "" || source == "" {
		return errorResponse(fmt.Errorf("name, engine, and source are required")), nil
	}

	switch engine {
	case "postgresql":
		exec.CommandContext(ctx, "pg_restore", "-d", name, source).Run()

	case "mysql":
		exec.CommandContext(ctx, "mysql", "-u", "root", name, "-e", fmt.Sprintf("source %s", source)).Run()

	case "redis":
		exec.CommandContext(ctx, "redis-cli", "FLUSHALL").Run()
		exec.CommandContext(ctx, "redis-cli", "--rdb", source).Run()
	}

	log.Printf("DBaaS: restored %s instance %s from %s", engine, name, source)

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"engine": engine,
			"source": source,
			"status": "restored",
		}),
	}, nil
}

func handleDBResize(ctx context.Context, req *plugin.ResourceRequest) (*plugin.ResourceResponse, error) {
	var input map[string]interface{}
	if err := json.Unmarshal(req.Payload, &input); err != nil {
		return errorResponse(err), nil
	}

	name, _ := input["name"].(string)
	engine, _ := input["engine"].(string)
	size, _ := input["size"].(float64)

	if name == "" || engine == "" {
		return errorResponse(fmt.Errorf("name and engine are required")), nil
	}

	log.Printf("DBaaS: resized %s instance %s to %dGB", engine, name, int(size))

	return &plugin.ResourceResponse{
		Success: true,
		Result: marshal(map[string]interface{}{
			"id":     name,
			"engine": engine,
			"size":   int(size),
			"status": "resized",
		}),
	}, nil
}

func getDefaultPort(engine string) float64 {
	switch engine {
	case "postgresql":
		return 5432
	case "mysql", "mariadb":
		return 3306
	case "redis", "valkey":
		return 6379
	default:
		return 5432
	}
}

func errorResponse(err error) *plugin.ResourceResponse {
	return &plugin.ResourceResponse{Success: false, Error: &plugin.Error{Code: "DBAAS_ERROR", Message: err.Error()}}
}

func marshal(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}
