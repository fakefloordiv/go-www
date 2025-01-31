package www

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	//"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var ErrorEmptyListValues = errors.New("an empty list of values is passed to create multipart content")

type Request struct {
	*http.Request
	client  *StandardClient
	err     error
	body    io.Reader
	params  string
	mime    string
	cookies []*http.Cookie
}

func NewRequest(client *StandardClient) *Request {
	return &Request{
		client: client,
	}
}

func (r Request) Error() error {
	return r.err
}

func (r Request) Headers() (out http.Header) {
	if r.Request != nil {
		out = r.Request.Header
	}

	return out
}

func (r Request) Cookies() (out []*http.Cookie) {
	if r.Request != nil {
		out = r.Request.Cookies()
	}

	return out
}

func (r *Request) SetCookies(cookies ...*http.Cookie) *Request {
	r.cookies = cookies
	return r
}

func (r *Request) prepareCookies() {
	for _, cookie := range r.cookies {
		r.Request.AddCookie(cookie)
	}
}

func (r *Request) prepareRequest(
	method string, uri string, headers ...http.Header) {

	var err error

	body, ok := r.body.(io.ReadCloser)
	if !ok && r.body != nil {
		body = io.NopCloser(r.body)
	}

	r.Request, err = http.NewRequest(method, uri, body)
	if err != nil {
		r.err = err
		return
	}

	r.Request.URL.RawQuery = r.params
	if r.mime != "" {
		r.Request.Header.Set("Content-Type", r.mime)
	}

	if len(headers) > 0 {
		for key, val := range headers[0] {
			r.Request.Header.Set(key, val[0])
		}
	}

}

func (r *Request) Get(uri string, headers ...http.Header) *Response {
	return r.Do(http.MethodGet, uri, headers...)
}

func (r *Request) Post(uri string, headers ...http.Header) *Response {
	return r.Do(http.MethodPost, uri, headers...)
}

func (r *Request) Put(uri string, headers ...http.Header) *Response {
	return r.Do(http.MethodPut, uri, headers...) // with body, output body
}

func (r *Request) Patch(uri string, headers ...http.Header) *Response {
	return r.Do(http.MethodPatch, uri, headers...) // with body, output body
}

func (r *Request) Delete(uri string, headers ...http.Header) *Response {
	return r.Do(http.MethodDelete, uri, headers...) // may have a body, output body
}

func (r *Request) Head(uri string) *Response {
	return r.Do(http.MethodHead, uri) // no body
}

func (r *Request) Trace(uri string) *Response {
	return r.Do(http.MethodTrace, uri) // no body
}

func (r *Request) Options(uri string) *Response {
	return r.Do(http.MethodOptions, uri) // no body
}

func (r *Request) Connect(uri string) *Response {
	return r.Do(http.MethodConnect, uri) // no body
}

func (r *Request) Do(method string, uri string, headers ...http.Header) *Response {
	var err error

	defer closeReader(r.body)

	if r.err != nil {
		return &Response{nil, r.err, nil}
	}

	r.prepareRequest(method, uri, headers...)
	r.prepareCookies()
	if r.err != nil {
		return &Response{nil, r.err, nil}
	}

	resp, err := r.client.Do(r.Request)

	return &Response{
		Response: resp,
		err:      err,
		content:  nil,
	}
}

func (r *Request) With(params *url.Values, data *url.Values) *Request {
	r.params = params.Encode()
	r.body = strings.NewReader(data.Encode())
	r.mime = "application/x-www-form-urlencoded"
	return r
}

func (r *Request) WithQuery(params *url.Values) *Request {
	r.params = params.Encode()
	return r
}

func (r *Request) WithForm(data *url.Values) *Request {
	r.mime = "application/x-www-form-urlencoded"
	r.body = strings.NewReader(data.Encode())
	return r
}

func (r *Request) Json(data interface{}) *Request {

	body, err := json.Marshal(data)
	if err != nil {
		r.err = err
		return r
	}
	r.mime = "application/json"
	r.body = bytes.NewReader(body)
	return r
}

func (r *Request) JSON(data interface{}) *Request {
	return r.Json(data)
}

func (r *Request) WithFile(reader io.Reader) *Request {
	r.mime = "binary/octet-stream"
	r.body = reader
	return r
}

func (r *Request) AttachFile(reader io.Reader, contentType ...string) *Request {
	var err error
	var fileName string
	var part io.Writer

	if f, ok := reader.(*os.File); ok {
		defer closeReader(reader)
		fileName = filepath.Base(f.Name())
	} else {
		r.err = err
		return r
	}

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	if part, err = CreateFormFile(
		writer, "file", fileName, contentType...); err != nil {
		r.err = err
		return r
	}

	_, err = io.Copy(part, reader)

	if err != nil {
		r.err = err
		return r
	}

	r.mime = writer.FormDataContentType()
	writer.Close()
	r.body = body

	return r
}

func (r *Request) AttachFiles(files map[string][]interface{}) *Request {
	var (
		err         error
		fileName    string
		contentType string
		part        io.Writer
	)

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	var closeReaders []io.Reader

	for field, values := range files {
		if len(values) == 0 {
			r.err = ErrorEmptyListValues
			return r
		}
		reader, ok := values[0].(io.Reader)
		if !ok {
			r.err = errors.New("value is not an interface io.Reader")
			continue
		}

		if len(values) > 1 {
			contentType, ok = values[1].(string)
			if !ok {
				r.err = errors.New("value is not a string")
				continue
			}
		}

		if f, ok := reader.(*os.File); ok {
			fileName = filepath.Base(f.Name())
			closeReaders = append(closeReaders, f)

			if part, err = CreateFormFile(
				writer, field, fileName, contentType); err != nil {
				r.err = err
				continue
			}
		} else {
			if part, err = writer.CreateFormField(field); err != nil {
				r.err = err
				continue
			}
		}
		if _, err = io.Copy(part, reader); err != nil {
			r.err = err
			continue
		}
	}

	r.mime = writer.FormDataContentType()
	writer.Close()
	r.body = body

	for _, reader := range closeReaders {
		closeReader(reader)
	}

	return r
}
