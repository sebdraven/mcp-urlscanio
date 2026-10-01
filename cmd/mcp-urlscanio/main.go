// Command mcp-urlscanio serves the urlscan.io search and artefact endpoints over MCP.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sebdraven/mcp-urlscanio/internal/mcptools"
	"github.com/sebdraven/mcp-urlscanio/internal/urlscan"
)

var version = "dev"

func main() {
	var (
		outDir  = flag.String("out", "", "default directory for saved artefacts")
		query   = flag.String("search", "", "run one search and exit")
		addr    = flag.String("http", "", "serve streamable HTTP on this address instead of stdio; no authentication, bind to 127.0.0.1 (e.g. 127.0.0.1:8080)")
		showVer = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		return
	}

	key, err := urlscan.Key()
	if err != nil {
		log.Fatalf("%v", err)
	}

	client := urlscan.New(key)
	ctx := context.Background()

	if *query != "" {
		page, err := client.Search(ctx, urlscan.SearchParams{Query: *query})
		if err != nil {
			log.Fatalf("%v", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(page); err != nil {
			log.Fatalf("encoding results: %v", err)
		}
		os.Exit(0)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "urlscan",
		Version: version,
	}, nil)
	mcptools.Register(server, client, *outDir)

	if *addr != "" {
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
		if err := http.ListenAndServe(*addr, handler); err != nil {
			log.Fatalf("http %s: %v", *addr, err)
		}
		return
	}

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
