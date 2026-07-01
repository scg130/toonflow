package adapter

import "testing"

func TestExtractImageURLOrB64_platformOutputs(t *testing.T) {
	u := "https://platform-outputs.agnes-ai.space/images/t2i/704a68ef61a04e77a48e98d72728ad6e.png"
	got, b, err := extractImageURLOrB64(map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"url": u},
		},
	})
	if err != nil || b != "" || got != u {
		t.Fatalf("got url=%q b64=%q err=%v", got, b, err)
	}
}

func TestExtractImageURLOrB64_imageURLField(t *testing.T) {
	u := "https://platform-outputs.agnes-ai.space/images/t2i/abc.png"
	got, b, err := extractImageURLOrB64(map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"image_url": u, "b64_json": nil},
		},
	})
	if err != nil || b != "" || got != u {
		t.Fatalf("got url=%q b64=%q err=%v", got, b, err)
	}
}

func TestExtractImageURLOrB64_dataURL(t *testing.T) {
	u, b, err := extractImageURLOrB64(map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"url": "https://storage.googleapis.com/agnes-aigc/x.png"},
		},
	})
	if err != nil || b != "" || u != "https://storage.googleapis.com/agnes-aigc/x.png" {
		t.Fatalf("got url=%q b64=%q err=%v", u, b, err)
	}
}

func TestExtractImageURLOrB64_output(t *testing.T) {
	u, b, err := extractImageURLOrB64(map[string]interface{}{
		"output": []interface{}{"https://cdn.example.com/img.png"},
	})
	if err != nil || b != "" || u != "https://cdn.example.com/img.png" {
		t.Fatalf("got url=%q b64=%q err=%v", u, b, err)
	}
}

func TestExtractImageURLOrB64_topLevelURL(t *testing.T) {
	u, b, err := extractImageURLOrB64(map[string]interface{}{
		"url": "https://cdn.example.com/top.png",
	})
	if err != nil || b != "" || u != "https://cdn.example.com/top.png" {
		t.Fatalf("got url=%q b64=%q err=%v", u, b, err)
	}
}

func TestExtractImageURLOrB64_dataStringURL(t *testing.T) {
	u, b, err := extractImageURLOrB64(map[string]interface{}{
		"data": []interface{}{"https://cdn.example.com/direct.png"},
	})
	if err != nil || b != "" || u != "https://cdn.example.com/direct.png" {
		t.Fatalf("got url=%q b64=%q err=%v", u, b, err)
	}
}

func TestExtractImageURLOrB64_bothURLAndB64(t *testing.T) {
	u := "https://platform-outputs.agnes-ai.space/images/t2i/abc.png"
	got, b, err := extractImageURLOrB64(map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"url": u, "b64_json": "aGVsbG8="},
		},
	})
	if err != nil || got != u || b != "aGVsbG8=" {
		t.Fatalf("got url=%q b64=%q err=%v", got, b, err)
	}
}

func TestExtractImageURLOrB64_base64Alias(t *testing.T) {
	u, b, err := extractImageURLOrB64(map[string]interface{}{
		"data": []interface{}{
			map[string]interface{}{"base64": "aGVsbG8="},
		},
	})
	if err != nil || u != "" || b != "aGVsbG8=" {
		t.Fatalf("got url=%q b64=%q err=%v", u, b, err)
	}
}

func TestExtractImageURLOrB64_apiError(t *testing.T) {
	_, _, err := extractImageURLOrB64(map[string]interface{}{
		"error": map[string]interface{}{"message": "rate limited"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
