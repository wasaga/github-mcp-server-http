package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	githubmcp "github.com/github/github-mcp-server/pkg/github"
	"github.com/github/github-mcp-server/pkg/translations"
	gogithub "github.com/google/go-github/v69/github"
	"github.com/mark3labs/mcp-go/server"
	"github.com/shurcooL/githubv4"
)

var version = "version"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(ctx context.Context) error {
	ghServer := githubmcp.NewServer(version)

	t, _ := translations.TranslationHelper()
	toolsets, err := githubmcp.InitToolsets(nil, true, getRESTClient, getGQLClient, t)
	if err != nil {
		return fmt.Errorf("failed to initialize toolsets: %w", err)
	}

	ct := githubmcp.InitContextToolset(getRESTClient, t)
	githubmcp.RegisterResources(ghServer, getRESTClient, t)
	toolsets.RegisterTools(ghServer)
	ct.RegisterTools(ghServer)

	server := server.NewStreamableHTTPServer(ghServer, server.WithHTTPContextFunc(tokenFromRequest))

	port, ok := os.LookupEnv("PORT")
	if !ok {
		port = "3020"
	}
	addr := fmt.Sprintf(":%s", port)
	log.Printf("Starting server on %s\n", addr)

	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Println("Shutting down server...")
		_ = server.Shutdown(ctx)
	}()
	return server.Start(addr)
}

type authKey struct{}

func tokenFromRequest(ctx context.Context, r *http.Request) context.Context {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ctx
	}
	prefix := "Bearer "
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
		return context.WithValue(ctx, authKey{}, auth[len(prefix):])
	}
	return ctx
}

func tokenFromContext(ctx context.Context) (string, error) {
	auth, ok := ctx.Value(authKey{}).(string)
	if !ok {
		return "", fmt.Errorf("missing auth")
	}
	return auth, nil
}

func getRESTClient(ctx context.Context) (*gogithub.Client, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token from context: %w", err)
	}

	client := gogithub.NewClient(nil).WithAuthToken(token)

	return client, nil
}

func getGQLClient(ctx context.Context) (*githubv4.Client, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get token from context: %w", err)
	}

	client := &http.Client{
		Transport: &bearerAuthTransport{
			transport: http.DefaultTransport,
			token:     token,
		},
	}
	return githubv4.NewEnterpriseClient("https://api.github.com/graphql", client), nil
}

type bearerAuthTransport struct {
	transport http.RoundTripper
	token     string
}

func (t *bearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.transport.RoundTrip(req)
}
