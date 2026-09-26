package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

const brandLogoURI = "https://assets.acme.example/brand/logo.png"

// brandLogoPNG is the start of a 32x32 RGB PNG: signature and IHDR chunk.
var brandLogoPNG = []byte{
	0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
	0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x20, 0x08, 0x02, 0x00, 0x00, 0x00,
	0xfc, 0x18, 0xed, 0xa3,
}

// logoServer serves brandLogoURI as a binary (blob) resource.
func logoServer() *officialMCP.Server {
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: "brand-assets", Version: "2.1.0"}, nil)
	server.AddResource(&officialMCP.Resource{URI: brandLogoURI, Name: "logo.png", MIMEType: "image/png"},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: brandLogoURI, MIMEType: "image/png", Blob: brandLogoPNG},
			}}, nil
		})
	return server
}

// A blob resource reaches `resource get` as the bytes the server sent: the
// SDK has already decoded the wire base64, so text output names the type
// and size without dumping bytes, and JSON output carries base64 again,
// as the spec's wire format does.
func TestResourceGet_BlobIsShownBySizeAndSentAsBase64(t *testing.T) {
	svc := connectHTTPService(t, logoServer(), "")
	result, err := svc.ReadResource(context.Background(), brandLogoURI)
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}

	text := captureStdout(t, func() { printResourceContentText(brandLogoURI, result.Contents) })
	if want := "Binary content: 33 bytes (base64 with --format json)"; !strings.Contains(text, want) {
		t.Errorf("text output lacks %q:\n%s", want, text)
	}
	if strings.Contains(text, "IHDR") || strings.Contains(text, "illegal base64") {
		t.Errorf("text output dumps or mis-decodes the blob:\n%s", text)
	}

	doc, err := json.Marshal(resourceReadOutput(brandLogoURI, result))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Contents []struct {
			Blob string `json:"blob"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(doc, &decoded); err != nil {
		t.Fatal(err)
	}
	if want := base64.StdEncoding.EncodeToString(brandLogoPNG); len(decoded.Contents) != 1 || decoded.Contents[0].Blob != want {
		t.Errorf("JSON = %s, want blob %q", doc, want)
	}
}
