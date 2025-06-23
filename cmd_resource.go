package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
)

var resourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "Manage MCP resources",
	Long:  "Commands for listing and reading resources on MCP servers",
}

var resourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available resources",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := client.ListResources(ctx, mcp.ListResourcesRequest{})
		if err != nil {
			return fmt.Errorf("failed to list resources: %w", err)
		}

		if outputJSON {
			data, err := json.MarshalIndent(result.Resources, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			fmt.Printf("Available Resources (%d):\n\n", len(result.Resources))
			for _, resource := range result.Resources {
				fmt.Printf("  %s\n", resource.Name)
				if resource.Description != "" {
					fmt.Printf("    %s\n", resource.Description)
				}
				if resource.URI != "" {
					fmt.Printf("    URI: %s\n", resource.URI)
				}
				if resource.MIMEType != "" {
					fmt.Printf("    Type: %s\n", resource.MIMEType)
				}
				fmt.Println()
			}
		}

		return nil
	},
}

var resourceReadCmd = &cobra.Command{
	Use:   "read <resource-uri>",
	Short: "Read a resource by URI",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resourceURI := args[0]

		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		request := mcp.ReadResourceRequest{}
		request.Params.URI = resourceURI

		result, err := client.ReadResource(ctx, request)
		if err != nil {
			return fmt.Errorf("failed to read resource: %w", err)
		}

		if outputJSON {
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			for i, content := range result.Contents {
				if i > 0 {
					fmt.Println("\n---")
				}
				
				// Check for different content types
				if textContent, ok := content.(mcp.TextResourceContents); ok {
					fmt.Printf("Resource: %s\n", textContent.URI)
					if textContent.MIMEType != "" {
						fmt.Printf("Type: %s\n", textContent.MIMEType)
					}
					fmt.Printf("\n%s\n", textContent.Text)
				} else if blobContent, ok := content.(mcp.BlobResourceContents); ok {
					fmt.Printf("Resource: %s\n", blobContent.URI)
					if blobContent.MIMEType != "" {
						fmt.Printf("Type: %s\n", blobContent.MIMEType)
					}
					fmt.Printf("Blob data: %s\n", blobContent.Blob)
				} else {
					fmt.Printf("%v\n", content)
				}
			}
		}

		return nil
	},
}

var resourceDescribeCmd = &cobra.Command{
	Use:   "describe <resource-name>",
	Short: "Show detailed information about a resource",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resourceName := args[0]

		client, err := createMCPClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		defer closeClientGracefully(client)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result, err := client.ListResources(ctx, mcp.ListResourcesRequest{})
		if err != nil {
			return fmt.Errorf("failed to list resources: %w", err)
		}

		var targetResource *mcp.Resource
		for _, resource := range result.Resources {
			if resource.Name == resourceName {
				targetResource = &resource
				break
			}
		}

		if targetResource == nil {
			return fmt.Errorf("resource '%s' not found", resourceName)
		}

		if outputJSON {
			data, err := json.MarshalIndent(targetResource, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
		} else {
			fmt.Printf("Resource: %s\n", targetResource.Name)
			if targetResource.Description != "" {
				fmt.Printf("Description: %s\n", targetResource.Description)
			}
			if targetResource.URI != "" {
				fmt.Printf("URI: %s\n", targetResource.URI)
			}
			if targetResource.MIMEType != "" {
				fmt.Printf("MIME Type: %s\n", targetResource.MIMEType)
			}
		}

		return nil
	},
}

func init() {
	resourceCmd.AddCommand(resourceListCmd)
	resourceCmd.AddCommand(resourceReadCmd)
	resourceCmd.AddCommand(resourceDescribeCmd)
}