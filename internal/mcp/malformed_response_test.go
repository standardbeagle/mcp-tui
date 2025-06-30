package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

// TestMalformedJSONResponses tests handling of various malformed JSON responses
func TestMalformedJSONResponses(t *testing.T) {
	t.Run("Incomplete_JSON", func(t *testing.T) {
		malformedResponses := []struct {
			name     string
			response string
		}{
			{"Missing_Closing_Brace", `{"protocolVersion":"2024-11-05","serverInfo":{"name":"test"`},
			{"Missing_Quote", `{"protocolVersion":"2024-11-05,"serverInfo":{"name":"test"}}`},
			{"Trailing_Comma", `{"protocolVersion":"2024-11-05","serverInfo":{"name":"test",},}`},
			{"Invalid_Escape", `{"protocolVersion":"2024-11-05","serverInfo":{"name":"tes\xt"}}`},
			{"Unexpected_Token", `{"protocolVersion":"2024-11-05";"serverInfo":{"name":"test"}}`},
			{"Empty_Response", ``},
			{"Only_Whitespace", `   `},
			{"Non_JSON_Text", `This is not JSON at all`},
			{"HTML_Response", `<html><body>Server Error</body></html>`},
		}

		for _, tc := range malformedResponses {
			t.Run(tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(tc.response))
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)
				assert.Error(t, err, "Connection should fail with malformed JSON: %s", tc.name)
				assert.False(t, service.IsConnected(), "Service should not be connected")

				// Error should indicate JSON parsing issue
				errorMsg := err.Error()
				assert.True(t,
					strings.Contains(errorMsg, "json") ||
						strings.Contains(errorMsg, "JSON") ||
						strings.Contains(errorMsg, "parse") ||
						strings.Contains(errorMsg, "decode"),
					"Error message should indicate JSON issue: %s", errorMsg)
			})
		}
	})

	t.Run("Invalid_JSON_Types", func(t *testing.T) {
		invalidTypeResponses := []struct {
			name     string
			response string
		}{
			{"String_Instead_Of_Object", `"this should be an object"`},
			{"Array_Instead_Of_Object", `["this", "should", "be", "an", "object"]`},
			{"Number_Instead_Of_Object", `42`},
			{"Boolean_Instead_Of_Object", `true`},
			{"Null_Response", `null`},
			{"Invalid_Protocol_Version", `{"protocolVersion":123,"serverInfo":{"name":"test"}}`},
			{"Missing_Required_Fields", `{"protocolVersion":"2024-11-05"}`},
			{"Wrong_Field_Types", `{"protocolVersion":"2024-11-05","serverInfo":"should be object"}`},
		}

		for _, tc := range invalidTypeResponses {
			t.Run(tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(tc.response))
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)
				assert.Error(t, err, "Connection should fail with invalid JSON types: %s", tc.name)
				assert.False(t, service.IsConnected(), "Service should not be connected")
			})
		}
	})

	t.Run("Truncated_Responses", func(t *testing.T) {
		// Test responses that are cut off at various points
		fullResponse := `{"protocolVersion":"2024-11-05","serverInfo":{"name":"test-server","version":"1.0.0"},"capabilities":{}}`

		truncationPoints := []int{10, 25, 50, len(fullResponse) - 10, len(fullResponse) - 1}

		for _, point := range truncationPoints {
			t.Run(fmt.Sprintf("Truncated_At_%d", point), func(t *testing.T) {
				truncatedResponse := fullResponse[:point]

				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(truncatedResponse))
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)
				assert.Error(t, err, "Connection should fail with truncated response at %d", point)
				assert.False(t, service.IsConnected(), "Service should not be connected")
			})
		}
	})

	t.Run("Extremely_Large_Responses", func(t *testing.T) {
		// Test handling of unreasonably large responses
		largeSizes := []int{1024 * 1024, 10 * 1024 * 1024} // 1MB, 10MB

		for _, size := range largeSizes {
			t.Run(fmt.Sprintf("Size_%dMB", size/(1024*1024)), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)

					// Create large JSON response
					w.Write([]byte(`{"protocolVersion":"2024-11-05","serverInfo":{"name":"test","data":"`))

					// Write large amount of data
					largeData := strings.Repeat("x", size)
					w.Write([]byte(largeData))

					w.Write([]byte(`"},"capabilities":{}}`))
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)
				// Connection may succeed or fail depending on implementation limits
				if err != nil {
					t.Logf("Large response handling failed as expected: %v", err)
				} else {
					t.Logf("Large response was handled successfully")
					service.Disconnect()
				}
			})
		}
	})
}

// TestCharacterEncodingIssues tests handling of character encoding problems
func TestCharacterEncodingIssues(t *testing.T) {
	t.Run("Invalid_UTF8_Sequences", func(t *testing.T) {
		invalidUTF8Sequences := []struct {
			name string
			data []byte
		}{
			{"Invalid_Start_Byte", []byte{0xFF, 0xFE}},
			{"Incomplete_Multibyte", []byte{0xC0}},
			{"Overlong_Encoding", []byte{0xC0, 0x80}}, // Overlong encoding of NULL
			{"Invalid_Continuation", []byte{0xC2, 0xFF}},
			{"Mixed_Valid_Invalid", append([]byte("valid text "), 0xFF, 0xFE)},
		}

		for _, tc := range invalidUTF8Sequences {
			t.Run(tc.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusOK)

					// Start with valid JSON structure
					w.Write([]byte(`{"protocolVersion":"2024-11-05","serverInfo":{"name":"`))

					// Insert invalid UTF-8
					w.Write(tc.data)

					// Complete JSON structure
					w.Write([]byte(`"},"capabilities":{}}`))
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)
				assert.Error(t, err, "Connection should fail with invalid UTF-8: %s", tc.name)
				assert.False(t, service.IsConnected(), "Service should not be connected")
			})
		}
	})

	t.Run("Unicode_Edge_Cases", func(t *testing.T) {
		unicodeTestCases := []struct {
			name string
			text string
		}{
			{"Null_Character", "test\x00null"},
			{"Control_Characters", "test\x01\x02\x03control"},
			{"High_Unicode", "test\U0001F600emoji"}, // 😀 emoji
			{"Mixed_Scripts", "English中文العربيةРусский"},
			{"Zero_Width_Characters", "test\u200Bzero\u200Cwidth"},
			{"Combining_Characters", "test\u0301\u0302combining"},
			{"Right_To_Left", "test\u202Ertl\u202C"},
		}

		for _, tc := range unicodeTestCases {
			t.Run(tc.name, func(t *testing.T) {
				if !utf8.ValidString(tc.text) {
					t.Skipf("Test string is not valid UTF-8: %s", tc.name)
				}

				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusOK)

					response := map[string]interface{}{
						"protocolVersion": "2024-11-05",
						"serverInfo": map[string]interface{}{
							"name":    tc.text,
							"version": "1.0.0",
						},
						"capabilities": map[string]interface{}{},
					}

					json.NewEncoder(w).Encode(response)
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)
				if err == nil {
					assert.True(t, service.IsConnected(), "Service should handle unicode correctly: %s", tc.name)
					service.Disconnect()
				} else {
					t.Logf("Unicode handling failed: %s - %v", tc.name, err)
				}
			})
		}
	})

	t.Run("Wrong_Content_Type", func(t *testing.T) {
		wrongContentTypes := []struct {
			contentType string
			description string
		}{
			{"text/plain", "Plain text content type"},
			{"text/html", "HTML content type"},
			{"application/xml", "XML content type"},
			{"application/octet-stream", "Binary content type"},
			{"", "Missing content type"},
			{"application/json; charset=iso-8859-1", "Wrong charset"},
		}

		for _, tc := range wrongContentTypes {
			t.Run(tc.description, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tc.contentType != "" {
						w.Header().Set("Content-Type", tc.contentType)
					}
					w.WriteHeader(http.StatusOK)

					// Valid JSON content regardless of content type
					json.NewEncoder(w).Encode(map[string]interface{}{
						"protocolVersion": "2024-11-05",
						"serverInfo": map[string]interface{}{
							"name":    "test-server",
							"version": "1.0.0",
						},
						"capabilities": map[string]interface{}{},
					})
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)
				// Some implementations may be lenient about content type
				if err != nil {
					t.Logf("Connection failed with wrong content type (%s): %v", tc.contentType, err)
				} else {
					t.Logf("Connection succeeded despite wrong content type: %s", tc.contentType)
					service.Disconnect()
				}
			})
		}
	})
}

// TestBinaryDataHandling tests handling of binary data in responses
func TestBinaryDataHandling(t *testing.T) {
	t.Run("Binary_Data_In_Response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			// Mix valid JSON with binary data
			validStart := `{"protocolVersion":"2024-11-05","serverInfo":{"name":"`
			binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
			validEnd := `"},"capabilities":{}}`

			w.Write([]byte(validStart))
			w.Write(binaryData)
			w.Write([]byte(validEnd))
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		assert.Error(t, err, "Connection should fail with binary data in JSON")
		assert.False(t, service.IsConnected(), "Service should not be connected")
	})

	t.Run("Base64_Encoded_Binary", func(t *testing.T) {
		// Test legitimate base64 encoded binary data
		binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
		base64Data := bytes.NewBuffer(nil)
		encoder := json.NewEncoder(base64Data)
		encoder.Encode(binaryData)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			response := map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{
					"name":    "binary-test-server",
					"version": "1.0.0",
					"data":    string(binaryData), // This might cause issues
				},
				"capabilities": map[string]interface{}{},
			}

			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		// This may succeed or fail depending on how binary data is handled
		if err != nil {
			t.Logf("Binary data handling failed as expected: %v", err)
		} else {
			t.Logf("Binary data was handled successfully")
			service.Disconnect()
		}
	})
}

// TestResponseSizeEdgeCases tests edge cases related to response sizes
func TestResponseSizeEdgeCases(t *testing.T) {
	t.Run("Empty_Response_Body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// No body written
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		assert.Error(t, err, "Connection should fail with empty response body")
		assert.False(t, service.IsConnected(), "Service should not be connected")
	})

	t.Run("Single_Character_Response", func(t *testing.T) {
		singleCharResponses := []string{"{", "}", "[", "]", "\"", "n", "t", "f"}

		for _, char := range singleCharResponses {
			t.Run(fmt.Sprintf("Char_%s", char), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(char))
				}))
				defer server.Close()

				service := NewService()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				connConfig := &config.ConnectionConfig{
					Type: config.TransportHTTP,
					URL:  server.URL,
				}

				err := service.Connect(ctx, connConfig)
				assert.Error(t, err, "Connection should fail with single character: %s", char)
				assert.False(t, service.IsConnected(), "Service should not be connected")
			})
		}
	})

	t.Run("Repeated_Field_Names", func(t *testing.T) {
		// JSON with duplicate keys (technically invalid but sometimes handled)
		duplicateKeyJSON := `{
			"protocolVersion": "2024-11-05",
			"protocolVersion": "invalid-duplicate",
			"serverInfo": {"name": "test"},
			"serverInfo": {"name": "duplicate", "version": "1.0.0"},
			"capabilities": {}
		}`

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(duplicateKeyJSON))
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		// Behavior with duplicate keys is implementation-dependent
		if err != nil {
			t.Logf("Duplicate keys rejected: %v", err)
		} else {
			t.Logf("Duplicate keys handled (last value wins)")
			service.Disconnect()
		}
	})
}

// TestStreamingResponseIssues tests issues with streaming responses
func TestStreamingResponseIssues(t *testing.T) {
	t.Run("Chunked_Transfer_Incomplete", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Transfer-Encoding", "chunked")
			w.WriteHeader(http.StatusOK)

			// Send partial chunk
			w.Write([]byte(`{"protocolVersion":"2024-11-05"`))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			// Simulate incomplete transfer by not finishing
			time.Sleep(100 * time.Millisecond)
			// Handler ends without completing the JSON
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		assert.Error(t, err, "Connection should fail with incomplete chunked transfer")
		assert.False(t, service.IsConnected(), "Service should not be connected")
	})

	t.Run("Content_Length_Mismatch", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := `{"protocolVersion":"2024-11-05","serverInfo":{"name":"test","version":"1.0.0"},"capabilities":{}}`

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(response)+100)) // Wrong length
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(response))
			// Don't write the extra bytes indicated by Content-Length
		}))
		defer server.Close()

		service := NewService()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		connConfig := &config.ConnectionConfig{
			Type: config.TransportHTTP,
			URL:  server.URL,
		}

		err := service.Connect(ctx, connConfig)
		// Some HTTP clients may timeout waiting for remaining bytes
		if err != nil {
			t.Logf("Content-Length mismatch handled: %v", err)
		} else {
			t.Logf("Content-Length mismatch was ignored")
			service.Disconnect()
		}
	})
}
