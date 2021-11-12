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

var ErrorEmptyListValues = errors.New("An empty list of values is passed to create multipart content")

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
	return &Request{client: client}
}

func (r *Request) Error() error {
	return r.err
}

func (r *Request) Headers() (out http.Header) {
	if r.Request != nil {
		out = r.Request.Header
	}
	return
}

func (r *Request) Cookies() (out []*http.Cookie) {
	if r.Request != nil {
		out = r.Request.Cookies()
	}
	return
}

/*
func (r *Request) SetHeaders(headers http.Header) *Request {
    for key, val := range headers {
        r.Request.Header.Set(key, val[0])
    }
    return r
}

func (r *Request) AddHeaders(headers http.Header) *Request {
    for key, val := range headers {
        r.Request.Header.Add(key, val[0])
    }
    return r
}
*/

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

	return r.Do("GET", uri, headers...)
}

func (r *Request) Post(
	uri string, headers ...http.Header) *Response {

	return r.Do("POST", uri, headers...)
}

func (r *Request) Put(uri string, headers ...http.Header) *Response {
	return r.Do("PUT", uri, headers...) // with body, output body
}

func (r *Request) Patch(uri string, headers ...http.Header) *Response {
	return r.Do("PATCH", uri, headers...) // with body, output body
}

func (r *Request) Delete(uri string, headers ...http.Header) *Response {
	return r.Do("DELETE", uri, headers...) // can have a body, output body
}

func (r *Request) Head(uri string) *Response {
	return r.Do("HEAD", uri) // no body
}

func (r *Request) Trace(uri string) *Response {
	return r.Do("TRACE", uri) // no body
}

func (r *Request) Options(uri string) *Response {
	return r.Do("OPTIONS", uri) // no body
}

func (r *Request) Connect(uri string) *Response {
	return r.Do("CONNECT", uri) // no body
}

func (r *Request) Do(method string,
	uri string, headers ...http.Header) *Response {

	var err error
	defer closeReader(r.body)
	if r.err != nil {
		return &Response{nil, nil, nil}
	}

	r.prepareRequest(method, uri, headers...)
	r.prepareCookies()
	if r.err != nil {
		return &Response{nil, nil, nil}
	}

	resp, err := r.client.Do(r.Request)

	return &Response{resp, err, nil}
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

	body := &bytes.Buffer{}

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

	body := &bytes.Buffer{}

	writer := multipart.NewWriter(body)

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
			defer func(reader io.Reader) {
				closeReader(reader)
			}(reader)

			if part, err = CreateFormFile(
				writer, field, fileName, contentType); err != nil {
				r.err = err
				//closeReader(reader)
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

		//closeReader(reader)
	}

	r.mime = writer.FormDataContentType()
	writer.Close()
	r.body = body

	return r
}
