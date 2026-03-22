package pages

import (
	"bytes"
	"fmt"
	"html/template"
)

var errorTmpl = template.Must(template.New("error").Parse(string(mustReadEmbed("frontend/error.html"))))

func mustReadEmbed(name string) []byte {
	data, err := frontend.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("embedded file %q not found: %v", name, err))
	}
	return data
}

type errorData struct {
	Status int
	Title  string
	Detail string
}

// ErrorPage renders a styled HTML error page and returns it as a
// complete HTTP response (status line + headers + body), ready to be
// written to a connection.
func ErrorPage(status int, title string, detail string) []byte {
	var body bytes.Buffer
	errorTmpl.Execute(&body, errorData{
		Status: status,
		Title:  title,
		Detail: detail,
	})

	var resp bytes.Buffer
	fmt.Fprintf(&resp, "HTTP/1.1 %d %s\r\n", status, title)
	fmt.Fprintf(&resp, "Content-Type: text/html; charset=utf-8\r\n")
	fmt.Fprintf(&resp, "Content-Length: %d\r\n", body.Len())
	fmt.Fprintf(&resp, "Connection: close\r\n")
	fmt.Fprintf(&resp, "\r\n")
	resp.Write(body.Bytes())

	return resp.Bytes()
}
