package driver

import (
	"testing"

	"github.com/horizon/orion/plugins/orchestration/stack"
)

func TestParseNativeTemplateOrdersDependencies(t *testing.T) {
	doc, order, err := parseNativeTemplate(&stack.Template{Format: "heat", Content: `
resources:
  subnet:
    type: orion.io/network.subnet
    depends_on: [network]
    properties:
      cidr: 10.0.0.0/24
  network:
    type: orion.io/network.network
`})
	if err != nil {
		t.Fatalf("parse native template: %v", err)
	}
	if len(doc.Resources) != 2 || len(order) != 2 || order[0] != "network" || order[1] != "subnet" {
		t.Fatalf("unexpected graph order: resources=%v order=%v", doc.Resources, order)
	}
}

func TestParseNativeTemplateRejectsUnknownDependency(t *testing.T) {
	_, _, err := parseNativeTemplate(&stack.Template{Format: "yaml", Content: `resources:
  subnet:
    type: orion.io/network.subnet
    depends_on: [missing]
`})
	if err == nil {
		t.Fatal("expected unknown dependency error")
	}
}
