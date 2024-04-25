package models

type Model struct {
	Description string `json:"description"`
	Engine      string `json:"engine"`
	Format      string `json:"format"`
	ID          string `json:"id"`
	Metadata    struct {
		Author string   `json:"author"`
		Size   int      `json:"size"`
		Tags   []string `json:"tags"`
	} `json:"metadata"`
	Name       string `json:"name"`
	Object     string `json:"object"`
	Parameters struct {
		FrequencyPenalty float64  `json:"frequency_penalty"`
		MaxTokens        int      `json:"max_tokens"`
		PresencePenalty  float64  `json:"presence_penalty"`
		Stop             []string `json:"stop"`
		Stream           bool     `json:"stream"`
		Temperature      float64  `json:"temperature"`
		TopP             float64  `json:"top_p"`
	} `json:"parameters"`
	Settings struct {
		CtxLen         int    `json:"ctx_len"`
		PromptTemplate string `json:"prompt_template"`
	} `json:"settings"`
	SourceURL string `json:"source_url"`
	Version   string `json:"version"`
}

type ModelStatusResponse struct {
	ID         string  `json:"id"`
	FileName   string  `json:"file_name"`
	Progress   float64 `json:"progress"`
	Status     string  `json:"status"`
	FolderPath string  `json:"folder_path"`
	Model      Model   `json:"model"`
	Type       string  `json:"type"`
}
