package screens

import (
	"fmt"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	imcp "github.com/standardbeagle/mcp-tui/internal/mcp"
)

func TestToolReExecutionVisual(t *testing.T) {
	t.Run("visual_re_execution_display", func(t *testing.T) {
		tool := mcp.Tool{
			Name:        "get-weather",
			Description: "Get current weather for a location",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"location": map[string]interface{}{
						"type":        "string",
						"description": "City name or zip code",
					},
				},
				Required: []string{"location"},
			},
		}

		ts := NewToolScreen(tool, nil)
		ts.fields[0].input.SetValue("New York")

		fmt.Println("\n=== Initial Tool Screen ===")
		fmt.Println(ts.View())

		// First execution
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"temp": "72°F", "condition": "Sunny"}`,
					},
				},
			},
		})

		fmt.Println("\n=== After First Execution ===")
		fmt.Println(ts.View())

		// Wait a bit for different timestamp
		time.Sleep(100 * time.Millisecond)

		// Second execution - same result
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"temp": "72°F", "condition": "Sunny"}`,
					},
				},
			},
		})

		fmt.Println("\n=== After Re-Execution (Same Result) ===")
		fmt.Println(ts.View())

		// Third execution - different result
		time.Sleep(100 * time.Millisecond)
		ts.Update(toolExecutionCompleteMsg{
			Result: &imcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: `{"temp": "75°F", "condition": "Partly Cloudy"}`,
					},
				},
			},
		})

		fmt.Println("\n=== After Third Execution (Different Result) ===")
		fmt.Println(ts.View())

		// Show status messages
		fmt.Println("\n=== Status Messages ===")
		for i := 1; i <= 3; i++ {
			ts.executionCount = i
			msg := fmt.Sprintf("Tool executed successfully (#%d)", i)
			if i > 1 {
				msg = fmt.Sprintf("Tool executed successfully (#%d) ✨", i)
			}
			fmt.Printf("Execution %d: %s\n", i, msg)
		}
	})
}
