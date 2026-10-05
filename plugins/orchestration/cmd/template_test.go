package main

import "testing"

func TestValidateDeclarativeTemplateRejectsUnknownDependency(t *testing.T) {
	err := validateDeclarativeTemplate("heat", "resources:\n  network:\n    type: Orion::Network\n    depends_on: [missing]\n")
	if err == nil {
		t.Fatal("expected unknown dependency to be rejected")
	}
}

func TestValidateDeclarativeTemplateRejectsCycles(t *testing.T) {
	err := validateDeclarativeTemplate("yaml", "resources:\n  first:\n    type: Orion::Network\n    depends_on: [second]\n  second:\n    type: Orion::Subnet\n    depends_on: [first]\n")
	if err == nil {
		t.Fatal("expected dependency cycle to be rejected")
	}
}

func TestValidateDeclarativeTemplateAllowsTerraformContent(t *testing.T) {
	if err := validateDeclarativeTemplate("terraform", "resource \"null_resource\" \"example\" {}\n"); err != nil {
		t.Fatalf("terraform content should be delegated to Terraform: %v", err)
	}
}
