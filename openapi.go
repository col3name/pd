// Package openapi embeds the OpenAPI contract for POST /process and exposes
// HTTP handlers that serve the specification and an interactive Swagger UI.
package openapi

import (
	_ "embed"
	"net/http"
)

// Spec is the full OpenAPI 3 document for the /process contract.
//
//go:embed process_api.yaml
var Spec []byte

const docsPage = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PII Module — OpenAPI /process</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
<style>
  body { margin: 0; background: #fafafa; }
  #banner { font: 14px/1.4 system-ui, sans-serif; padding: 10px 16px; background: #1b1b1b; color: #fff; }
  #banner a { color: #7cc4ff; }
</style>
</head>
<body>
<div id="banner">
  <strong>Модуль безопасности ПД</strong> — интерактивная спецификация:
  <a href="/openapi.yaml">/openapi.yaml</a> ·
  <a href="/process_api.yaml">/process_api.yaml</a> ·
  <a href="/health">/health</a> ·
  <a href="/metrics">/metrics</a>
  — нажмите <em>Try it out</em>, введите payload/payload_id и отправьте запрос.
</div>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  window.onload = () => {
    window.ui = SwaggerUIBundle({
      url: '/openapi.yaml',
      dom_id: '#swagger-ui',
      deepLinking: true,
      tryItOutEnabled: true,
      persistAuthorization: true,
      presets: [SwaggerUIBundle.presets.apis],
      layout: 'BaseLayout'
    });
  };
</script>
</body>
</html>
`

// SpecHandler serves the OpenAPI document as YAML.
func SpecHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(Spec)
	}
}

// DocsHandler serves an interactive Swagger UI page bound to SpecHandler.
// The UI loads the spec from /openapi.yaml with the relative server URL "/"
// so "Try it out" requests go back to the current host.
func DocsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsPage))
	}
}
