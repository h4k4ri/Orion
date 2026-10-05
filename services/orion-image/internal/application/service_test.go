package application

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/horizon/orion/libs/go/kit/image"
)

func TestCreateUploadAndDeleteImage(t *testing.T) {
	dir := t.TempDir()
	service := &Service{storeDir: dir, images: map[string]image.Image{}}
	created, err := service.CreateImage(context.Background(), image.Image{Name: "test", DiskFormat: "raw", ContainerFormat: "bare"})
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("orion-image")
	uploaded, err := service.UploadImage(context.Background(), created.ID, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.Status != "active" || uploaded.SizeBytes != int64(len(content)) || uploaded.ChecksumSHA256 == "" {
		t.Fatalf("unexpected uploaded image: %#v", uploaded)
	}
	if _, err := os.Stat(uploaded.Path); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteImage(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetImage(context.Background(), created.ID); err == nil {
		t.Fatal("expected image to be deleted")
	}
}
