package upstream

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/murongg/SubLane/internal/accounts"
)

// MaxImageBody bounds Images responses, which carry base64 images and exceed text response limits.
const MaxImageBody = 64 << 20

// Codex subscriptions expose built-in image models through ChatGPT's direct Images endpoints.
// The list follows CLIProxyAPI's built-in Codex image definitions.
var codexImageModels = []string{
	"gpt-image-1.5",
	"gpt-image-2",
	"gpt-image-2.5",
	"gpt-image-2.5-flare",
	"gpt-image-2.5-sunburst",
	"gpt-image-2.6",
	"gpt-image-2.6-flare",
	"gpt-image-2.6-sunburst",
}

// IsImageModel reports whether a native model ID is a built-in Codex image model.
func IsImageModel(model string) bool {
	return slices.Contains(codexImageModels, strings.ToLower(strings.TrimSpace(model)))
}

// appendImageModels adds built-in image capabilities that the Codex text catalog does not advertise.
// Only catalogs that advertise GPT models gain them, so empty or unusual observations never imply support.
func appendImageModels(models []Model) []Model {
	if !slices.ContainsFunc(models, func(model Model) bool { return strings.HasPrefix(model.ID, "gpt-") && !IsImageModel(model.ID) }) {
		return models
	}
	images := true
	for _, id := range codexImageModels {
		if !slices.ContainsFunc(models, func(model Model) bool { return model.ID == id }) {
			models = append(models, Model{ModelCapabilities: accounts.ModelCapabilities{SupportsImages: &images}, ID: id, Object: "model", OwnedBy: "openai"})
		}
	}
	return models
}

// Images forwards an OpenAI Images request to the Codex backend without buffering the client stream.
func (c *Client) Images(ctx context.Context, credential accounts.Credential, raw []byte, headers http.Header, edit bool) (*Stream, error) {
	if credential.Kind() != "codex" {
		return nil, ErrInput
	}
	ctx = credentialContext(ctx, credential)
	var input map[string]json.RawMessage
	if int64(len(raw)) > c.MaxRequestBody() || json.Unmarshal(raw, &input) != nil || input == nil {
		return nil, ErrInput
	}
	var model string
	if json.Unmarshal(input["model"], &model) != nil || !IsImageModel(model) {
		return nil, ErrInput
	}
	// The gateway returns a single JSON document; partial-image streaming is not supported.
	delete(input, "stream")
	delete(input, "partial_images")
	body, err := json.Marshal(input)
	if err != nil {
		return nil, ErrInput
	}
	path := "/images/generations"
	if edit {
		path = "/images/edits"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, ErrInput
	}
	upstreamHeaders(request, credential, headers, false, c.codexVersion())
	response, err := c.inference.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUpstream
	}
	// Images responses are complete JSON documents; they never use stream translation state.
	return &Stream{Response: response}, nil
}

// MultipartImageEdit converts an OpenAI multipart image edit into the JSON form the Codex endpoint accepts.
// Uploaded images and masks become data URLs; the caller bounds the total request size.
func MultipartImageEdit(raw []byte, contentType string) ([]byte, error) {
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil || media != "multipart/form-data" || params["boundary"] == "" {
		return nil, ErrInput
	}
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	output := map[string]any{}
	images := []map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrInput
		}
		name := part.FormName()
		value, err := io.ReadAll(io.LimitReader(part, int64(len(raw))+1))
		part.Close()
		if err != nil {
			return nil, ErrInput
		}
		if part.FileName() != "" {
			kind := part.Header.Get("Content-Type")
			if kind == "" || kind == "application/octet-stream" {
				kind = http.DetectContentType(value)
			}
			if !strings.HasPrefix(kind, "image/") {
				return nil, ErrInput
			}
			url := "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(value)
			switch name {
			case "image", "image[]":
				images = append(images, map[string]string{"image_url": url})
			case "mask":
				output["mask"] = map[string]string{"image_url": url}
			default:
				return nil, ErrInput
			}
			continue
		}
		text := strings.TrimSpace(string(value))
		switch name {
		case "n", "output_compression":
			number, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return nil, ErrInput
			}
			output[name] = number
		case "model", "prompt", "size", "quality", "background", "output_format", "input_fidelity", "moderation", "response_format", "user":
			output[name] = text
		case "stream", "partial_images":
			// Streaming is rejected by the gateway; drop unsupported form fields consistently with JSON.
		default:
			return nil, ErrInput
		}
	}
	if len(images) > 0 {
		output["images"] = images
	}
	return json.Marshal(output)
}
