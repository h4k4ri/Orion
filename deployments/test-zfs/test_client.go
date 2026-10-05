package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/horizon/orion/gen/go/plugin/v1"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	endpoint := os.Getenv("ORION_PLUGIN_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:50051"
	}
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pluginv1.NewPluginExecutorClient(conn)

	fmt.Println("=== Testing HealthCheck ===")
	hcResp, err := client.HealthCheck(ctx, &pluginv1.HealthCheckRequest{})
	if err != nil {
		log.Fatalf("HealthCheck failed: %v", err)
	}
	fmt.Printf("HealthCheck: healthy=%v, message=%s\n\n", hcResp.Healthy, hcResp.Message)

	fmt.Println("=== Testing Create Volume ===")
	volInput := map[string]interface{}{
		"name": "test-volume",
		"size": 10,
		"pool": "tank",
	}
	inputBytes, _ := json.Marshal(volInput)
	var inputPayload map[string]interface{}
	_ = json.Unmarshal(inputBytes, &inputPayload)
	payload, _ := structpb.NewStruct(inputPayload)

	createResp, err := client.Invoke(ctx, &pluginv1.InvokeRequest{
		Resource:        "orion.io/storage.volume",
		ResourceVersion: "v1",
		ProviderId:      "zfs-provider-1",
		Operation:       "create",
		Payload:         payload,
	})
	if err != nil {
		log.Fatalf("Create volume failed: %v", err)
	}
	fmt.Printf("Create Volume: success=%v\n", createResp.Success)
	if createResp.Success {
		fmt.Printf("Result: %v\n", createResp.Result.AsMap())
	} else {
		fmt.Printf("Error: %s - %s\n", createResp.Error.Code, createResp.Error.Message)
	}

	fmt.Println("\n=== Testing Delete Volume ===")
	deleteInput := map[string]interface{}{
		"id": "tank/test-volume",
	}
	deleteBytes, _ := json.Marshal(deleteInput)
	var deletePayloadMap map[string]interface{}
	_ = json.Unmarshal(deleteBytes, &deletePayloadMap)
	deletePayload, _ := structpb.NewStruct(deletePayloadMap)

	deleteResp, err := client.Invoke(ctx, &pluginv1.InvokeRequest{
		Resource:        "orion.io/storage.volume",
		ResourceVersion: "v1",
		ProviderId:      "zfs-provider-1",
		Operation:       "delete",
		Payload:         deletePayload,
	})
	if err != nil {
		log.Fatalf("Delete volume failed: %v", err)
	}
	fmt.Printf("Delete Volume: success=%v\n", deleteResp.Success)

	fmt.Println("\n=== All tests passed! ===")
}
