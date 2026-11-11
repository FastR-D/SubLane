package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/murongg/SubLane/internal/upstream"
)

func TestGatewayForwardsCodexImageRequests(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var bodies []map[string]any
	fixture := newForwardFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error("upstream image body", err)
		}
		mu.Lock()
		paths, bodies = append(paths, r.URL.Path), append(bodies, body)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer synthetic-upstream-access" || r.Header.Get("Chatgpt-Account-Id") == "" {
			t.Error("image request lost Codex credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"created":1,"data":[{"b64_json":"c3ludGhldGlj"}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`)
	})
	ctx := t.Context()
	rows, err := fixture.accounts.List(ctx)
	if err != nil || len(rows) == 0 {
		t.Fatal(rows, err)
	}
	if err := fixture.accounts.SaveCatalog(ctx, rows[0].ID, 0, []string{"synthetic-model", "gpt-image-2"}, time.Now().Unix(), upstream.CatalogSource("codex")); err != nil {
		t.Fatal(err)
	}
	send := func(path, contentType string, body []byte) (int, string) {
		t.Helper()
		request, _ := http.NewRequest("POST", fixture.server.URL+path, bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+fixture.secret)
		request.Header.Set("Content-Type", contentType)
		response, err := fixture.server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(raw)
	}

	status, body := send("/v1/images/generations", "application/json", []byte(`{"model":"gpt-image-2","prompt":"Synthetic lighthouse","size":"1024x1024","prompt_cache_key":"client-key"}`))
	if status != 200 || !strings.Contains(body, `"b64_json":"c3ludGhldGlj"`) {
		t.Fatal("generation", status, body)
	}

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	_ = writer.WriteField("model", "gpt-image-2")
	_ = writer.WriteField("prompt", "Synthetic edit")
	_ = writer.WriteField("n", "1")
	file, _ := writer.CreateFormFile("image[]", "synthetic.png")
	_, _ = file.Write([]byte("\x89PNG\r\n\x1a\nsynthetic"))
	_ = writer.Close()
	if status, body := send("/v1/images/edits", writer.FormDataContentType(), form.Bytes()); status != 200 {
		t.Fatal("edit", status, body)
	}

	mu.Lock()
	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/codex/images/generations") || !strings.HasSuffix(paths[1], "/codex/images/edits") {
		t.Fatal("upstream paths", paths)
	}
	if bodies[0]["model"] != "gpt-image-2" || bodies[0]["prompt"] != "Synthetic lighthouse" || bodies[0]["prompt_cache_key"] != nil || bodies[0]["stream"] != nil {
		t.Fatal("generation body", bodies[0])
	}
	images, _ := bodies[1]["images"].([]any)
	first, _ := images[0].(map[string]any)
	if url, _ := first["image_url"].(string); bodies[1]["n"] != float64(1) || !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatal("edit body", bodies[1])
	}
	mu.Unlock()

	for _, rejected := range []struct{ path, body string }{
		{"/v1/images/generations", `{"model":"gpt-image-2","prompt":"x","stream":true}`},
		{"/v1/images/generations", `{"model":"synthetic-model","prompt":"x"}`},
		{"/v1/responses", `{"model":"gpt-image-2","input":"x"}`},
	} {
		if status, body := send(rejected.path, "application/json", []byte(rejected.body)); status != 400 {
			t.Fatal("unsupported image request", rejected, status, body)
		}
	}
	if status, _ := send("/v1/images/generations", "multipart/form-data; boundary=x", []byte("--x--")); status != 415 {
		t.Fatal("generation accepted multipart", status)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		var count, input int64
		_ = fixture.connection.QueryRow(`SELECT count(*), COALESCE(sum(input_tokens),0) FROM request_records WHERE operation='images' AND outcome='success'`).Scan(&count, &input)
		if count == 2 && input == 10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("image history", count, input)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
