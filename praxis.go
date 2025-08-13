package praxis

const Version = "v0.1.0"

type Client struct {
	apiKey string
	baseURL string
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		baseURL: "https://api.praxis.ai",
	}
}