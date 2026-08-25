package multipart

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
)

type Field struct {
	Name  string
	Value string
}

type File struct {
	FieldName   string
	FileName    string
	ContentType string
	Body        io.ReadCloser
}

func Body(fields []Field, file File) (io.ReadCloser, string) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType()

	go func() {
		defer file.Body.Close()
		pw.CloseWithError(write(mw, fields, file))
	}()
	return pr, contentType
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func write(mw *multipart.Writer, fields []Field, file File) error {
	for _, f := range fields {
		if err := mw.WriteField(f.Name, f.Value); err != nil {
			return err
		}
	}

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		quoteEscaper.Replace(file.FieldName), quoteEscaper.Replace(file.FileName)))
	h.Set("Content-Type", file.ContentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file.Body); err != nil {
		return err
	}
	return mw.Close()
}
