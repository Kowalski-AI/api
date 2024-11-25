package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Config struct {
	APIKey      string
	OpenAIKey   string
	GithubToken string
	Port        string
}

type PRAnalysisRequest struct {
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	PRNumber  int    `json:"pr_number"`
	ModelType string `json:"model_type"` // "openai" or "claude"
}

type PRAnalysisResponse struct {
	Analysis  string    `json:"analysis"`
	Timestamp time.Time `json:"timestamp"`
	ModelUsed string    `json:"model_used"`
}

func loadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading .env file: %v", err)
	}

	return &Config{
		APIKey:      os.Getenv("API_KEY"),
		OpenAIKey:   os.Getenv("OPENAI_API_KEY"),
		GithubToken: os.Getenv("GITHUB_TOKEN"),
		Port:        os.Getenv("PORT"),
	}, nil
}

func validateAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		config, err := loadConfig()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if apiKey != config.APIKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func analyzePRHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PRAnalysisRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Fetch PR changes from GitHub
	changes, err := fetchPRChanges(req.Owner, req.Repo, req.PRNumber)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching PR changes: %v", err), http.StatusInternalServerError)
		return
	}

	// Analyze changes using specified model
	analysis, err := analyzeChanges(changes, req.ModelType)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error analyzing changes: %v", err), http.StatusInternalServerError)
		return
	}

	response := PRAnalysisResponse{
		Analysis:  analysis,
		Timestamp: time.Now(),
		ModelUsed: req.ModelType,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func fetchPRChanges(owner, repo string, prNumber int) (string, error) {
	log.Printf("Fetching PR changes for %s/%s #%d", owner, repo, prNumber)
	config, err := loadConfig()
	if err != nil {
		log.Printf("Config loading failed: %v", err)
		return "", err
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNumber)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		return "", err
	}

	req.Header.Set("Authorization", "token "+config.GithubToken)
	req.Header.Set("Accept", "application/vnd.github.v3.diff")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to make request to github api: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		bodyString := string(bodyBytes)
		log.Printf("GitHub API returned status: %d %v", resp.StatusCode, bodyString)
		return "", fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	log.Printf("PR changes fetched successfully")

	var diffBuilder strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			diffBuilder.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	return diffBuilder.String(), nil
}

func analyzeChanges(changes, modelType string) (string, error) {
	config, err := loadConfig()
	if err != nil {
		return "", err
	}

	switch modelType {
	case "openai":
		client := openai.NewClient(config.OpenAIKey)
		resp, err := client.CreateChatCompletion(
			context.Background(),
			openai.ChatCompletionRequest{
				Model: openai.GPT4,
				Messages: []openai.ChatCompletionMessage{
					{
						Role: openai.ChatMessageRoleUser,
						Content: fmt.Sprintf(
							"Please analyze the following code changes and provide a detailed review:\n\n%s",
							changes,
						),
					},
				},
			},
		)
		if err != nil {
			return "", err
		}
		return resp.Choices[0].Message.Content, nil

	case "claude":
		// Implement Claude API integration here
		return "", fmt.Errorf("Claude integration not implemented yet")

	default:
		return "", fmt.Errorf("unsupported model type: %s", modelType)
	}
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	http.HandleFunc("/analyze-pr", validateAPIKey(analyzePRHandler))

	port := config.Port
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
