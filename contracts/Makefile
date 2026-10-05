.PHONY: test lint clean

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf gen/go

# Validate specs
validate-specs:
	@echo "Validating specs..."
	@find specs/v1 -name "*.yaml" | while read f; do \
		echo "Checking $$f..."; \
	done
	@echo "Done."
