package spdy

// A Request represents an HTTP request received by a server
// or to be sent by a client.
type Request struct {
  Method string // GET, POST, PUT, etc.

  // URL is created from the URI supplied on the Request-Line
  // as stored in RequestURI.
  URL *url.URL

  // The protocol version for incoming requests.
  // Outgoing requests always use HTTP/1.1.
  Proto      string // "HTTP/1.0"
  ProtoMajor int    // 1
  ProtoMinor int    // 0

  // A header maps request lines to their values.
  // If the header says
  //
  //      accept-encoding: gzip, deflate
  //      Accept-Language: en-us
  //      Connection: keep-alive
  //
  // then
  //
  //      Header = map[string][]string{
  //              "Accept-Encoding": {"gzip, deflate"},
  //              "Accept-Language": {"en-us"},
  //              "Connection": {"keep-alive"},
  //      }
  //
  // HTTP defines that header names are case-insensitive.
  // The request parser implements this by canonicalizing the
  // name, making the first character and any characters
  // following a hyphen uppercase and the rest lowercase.
  Header Header

  // The message body.
  Body io.ReadCloser

  // ContentLength records the length of the associated content.
  // The value -1 indicates that the length is unknown.
  // Values >= 0 indicate that the given number of bytes may
  // be read from Body.
  // For outgoing requests, a value of 0 means unknown if Body is not nil.
  ContentLength int64

  // The host on which the URL is sought.
  // Per SPDY draft 3, this is either the value of the :host header
  // or the host name given in the URL itself.
  // It may be of the form "host:port".
  Host string

  // Form contains the parsed form data, including both the URL
  // field's query parameters and the POST or PUT form data.
  // This field is only available after ParseForm is called.
  // The HTTP client ignores Form and uses Body instead.
  Form url.Values

  // PostForm contains the parsed form data from POST or PUT
  // body parameters.
  // This field is only available after ParseForm is called.
  // The HTTP client ignores PostForm and uses Body instead.
  PostForm url.Values

  // MultipartForm is the parsed multipart form, including file uploads.
  // This field is only available after ParseMultipartForm is called.
  // The HTTP client ignores MultipartForm and uses Body instead.
  MultipartForm *multipart.Form

  // Trailer maps trailer keys to values.  Like for Header, if the
  // response has multiple trailer lines with the same key, they will be
  // concatenated, delimited by commas.
  // For server requests, Trailer is only populated after Body has been
  // closed or fully consumed.
  // Trailer support is only partially complete.
  Trailer Header

  // RemoteAddr allows HTTP servers and other software to record
  // the network address that sent the request, usually for
  // logging. This field is not filled in by ReadRequest and
  // has no defined format. The HTTP server in this package
  // sets RemoteAddr to an "IP:port" address before invoking a
  // handler.
  // This field is ignored by the HTTP client.
  RemoteAddr string

  // RequestURI is the unmodified Request-URI of the
  // Request-Line (RFC 2616, Section 5.1) as sent by the client
  // to a server. Usually the URL field should be used instead.
  // It is an error to set this field in an HTTP client request.
  RequestURI string

  // TLS allows HTTP servers and other software to record
  // information about the TLS connection on which the request
  // was received. This field is not filled in by ReadRequest.
  // The HTTP server in this package sets the field for
  // TLS-enabled connections before invoking a handler.
  TLS *tls.ConnectionState
}

// ProtoAtLeast returns whether the HTTP protocol used
// in the request is at least major.minor.
func (r *Request) ProtoAtLeast(major, minor int) bool {
  return r.ProtoMajor > major ||
    r.ProtoMajor == major && r.ProtoMinor >= minor
}

// UserAgent returns the client's User-Agent, if sent in the request.
func (r *Request) UserAgent() string {
  return r.Header.Get("User-Agent")
}

// Cookies parses and returns the HTTP cookies sent with the request.
func (r *Request) Cookies() []*Cookie {
  return readCookies(r.Header, "")
}

var ErrNoCookie = errors.New("spdy: named cookie not present")

// Cookie returns the named cookie provided in the request or
// ErrNoCookie if not found.
func (r *Request) Cookie(name string) (*Cookie, error) {
  for _, c := range readCookies(r.Header, name) {
    return c, nil
  }
  return nil, ErrNoCookie
}

// AddCookie adds a cookie to the request.  Per RFC 6265 section 5.4,
// AddCookie does not attach more than one Cookie header field.  That
// means all cookies, if any, are written into the same line,
// separated by semicolon.
func (r *Request) AddCookie(c *Cookie) {
  s := fmt.Sprintf("%s=%s", sanitizeName(c.Name), sanitizeValue(c.Value))
  if c := r.Header.Get("Cookie"); c != "" {
    r.Header.Set("Cookie", c+"; "+s)
  } else {
    r.Header.Set("Cookie", s)
  }
}

// Referer returns the referring URL, if sent in the request.
//
// Referer is misspelled as in the request itself, a mistake from the
// earliest days of HTTP.  This value can also be fetched from the
// Header map as Header["Referer"]; the benefit of making it available
// as a method is that the compiler can diagnose programs that use the
// alternate (correct English) spelling req.Referrer() but cannot
// diagnose programs that use Header["Referrer"].
func (r *Request) Referer() string {
  return r.Header.Get("Referer")
}

// multipartByReader is a sentinel value.
// Its presence in Request.MultipartForm indicates that parsing of the request
// body has been handed off to a MultipartReader instead of ParseMultipartFrom.
var multipartByReader = &multipart.Form{
  Value: make(map[string][]string),
  File:  make(map[string][]*multipart.FileHeader),
}

// MultipartReader returns a MIME multipart reader if this is a
// multipart/form-data POST request, else returns nil and an error.
// Use this function instead of ParseMultipartForm to
// process the request body as a stream.
func (r *Request) MultipartReader() (*multipart.Reader, error) {
  if r.MultipartForm == multipartByReader {
    return nil, errors.New("spdy: MultipartReader called twice")
  }
  if r.MultipartForm != nil {
    return nil, errors.New("spdy: multipart handled by ParseMultipartForm")
  }
  r.MultipartForm = multipartByReader
  return r.multipartReader()
}

func (r *Request) multipartReader() (*multipart.Reader, error) {
  v := r.Header.Get("Content-Type")
  if v == "" {
    return nil, ErrNotMultipart
  }
  d, params, err := mime.ParseMediaType(v)
  if err != nil || d != "multipart/form-data" {
    return nil, ErrNotMultipart
  }
  boundary, ok := params["boundary"]
  if !ok {
    return nil, ErrMissingBoundary
  }
  return multipart.NewReader(r.Body, boundary), nil
}

// Return value if nonempty, def otherwise.
func valueOrDefault(value, def string) string {
  if value != "" {
    return value
  }
  return def
}

const defaultUserAgent = "Go 1.1 package github.com/SlyMarbo/spdy"

// Write writes an HTTP/1.1 request -- header and body -- in wire format.
// This method consults the following fields of the request:
//      Host
//      URL
//      Method (defaults to "GET")
//      Header
//      ContentLength
//      TransferEncoding
//      Body
//
// If Body is present, Content-Length is <= 0 and TransferEncoding
// hasn't been set to "identity", Write adds "Transfer-Encoding:
// chunked" to the header. Body is closed after it is sent.
// func (r *Request) Write(w io.Writer) error {
//   return r.write(w, false, nil)
// }

// WriteProxy is like Write but writes the request in the form
// expected by an HTTP proxy.  In particular, WriteProxy writes the
// initial Request-URI line of the request with an absolute URI, per
// section 5.1.2 of RFC 2616, including the scheme and host.
// In either case, WriteProxy also writes a Host header, using
// either r.Host or r.URL.Host.
// func (r *Request) WriteProxy(w io.Writer) error {
//   return r.write(w, true, nil)
// }

// TODO(Marbo): Add tie-in with spdy.
// extraHeaders may be nil
// func (req *Request) write(w io.Writer, usingProxy bool, extraHeaders Header) error {
//   host := req.Host
//   if host == "" {
//     if req.URL == nil {
//       return errors.New("spdy: Request.Write on Request with no Host or URL set")
//     }
//     host = req.URL.Host
//   }
// 
//   ruri := req.URL.RequestURI()
//   if usingProxy && req.URL.Scheme != "" && req.URL.Opaque == "" {
//     ruri = req.URL.Scheme + "://" + host + ruri
//   } else if req.Method == "CONNECT" && req.URL.Path == "" {
//     // CONNECT requests normally give just the host and port, not a full URL.
//     ruri = host
//   }
//   // TODO(bradfitz): escape at least newlines in ruri?
// 
//   // Wrap the writer in a bufio Writer if it's not already buffered.
//   // Don't always call NewWriter, as that forces a bytes.Buffer
//   // and other small bufio Writers to have a minimum 4k buffer
//   // size.
//   var bw *bufio.Writer
//   if _, ok := w.(io.ByteWriter); !ok {
//     bw = bufio.NewWriter(w)
//     w = bw
//   }
// 
//   fmt.Fprintf(w, "%s %s HTTP/1.1\r\n", valueOrDefault(req.Method, "GET"), ruri)
// 
//   // Header lines
//   fmt.Fprintf(w, "Host: %s\r\n", host)
// 
//   // Use the defaultUserAgent unless the Header contains one, which
//   // may be blank to not send the header.
//   userAgent := defaultUserAgent
//   if req.Header != nil {
//     if ua := req.Header["User-Agent"]; len(ua) > 0 {
//       userAgent = ua[0]
//     }
//   }
//   if userAgent != "" {
//     fmt.Fprintf(w, "User-Agent: %s\r\n", userAgent)
//   }
// 
//   // Process Body,ContentLength,Close,Trailer
//   tw, err := newTransferWriter(req)
//   if err != nil {
//     return err
//   }
//   err = tw.WriteHeader(w)
//   if err != nil {
//     return err
//   }
// 
//   // TODO: split long values?  (If so, should share code with Conn.Write)
//   err = req.Header.WriteSubset(w, reqWriteExcludeHeader)
//   if err != nil {
//     return err
//   }
// 
//   if extraHeaders != nil {
//     err = extraHeaders.Write(w)
//     if err != nil {
//       return err
//     }
//   }
// 
//   io.WriteString(w, "\r\n")
// 
//   // Write body and trailer
//   err = tw.WriteBody(w)
//   if err != nil {
//     return err
//   }
// 
//   if bw != nil {
//     return bw.Flush()
//   }
//   return nil
// }

// NewRequest returns a new Request given a method, URL, and optional body.
func NewRequest(method, urlStr string, body io.Reader) (*Request, error) {
  u, err := url.Parse(urlStr)
  if err != nil {
    return nil, err
  }
  rc, ok := body.(io.ReadCloser)
  if !ok && body != nil {
    rc = ioutil.NopCloser(body)
  }
  req := &Request{
    Method:     method,
    URL:        u,
    Proto:      "HTTP/1.1",
    ProtoMajor: 1,
    ProtoMinor: 1,
    Header:     make(Header),
    Body:       rc,
    Host:       u.Host,
  }
  if body != nil {
    switch v := body.(type) {
    case *bytes.Buffer:
      req.ContentLength = int64(v.Len())
    case *bytes.Reader:
      req.ContentLength = int64(v.Len())
    case *strings.Reader:
      req.ContentLength = int64(v.Len())
    }
  }

  return req, nil
}

// SetBasicAuth sets the request's Authorization header to use HTTP
// Basic Authentication with the provided username and password.
//
// With HTTP Basic Authentication the provided username and password
// are not encrypted.
func (r *Request) SetBasicAuth(username, password string) {
  s := username + ":" + password
  r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(s)))
}

// ParseForm parses the raw query from the URL and updates r.Form.
//
// For POST or PUT requests, it also parses the request body as a form and
// put the results into both r.PostForm and r.Form.
// POST and PUT body parameters take precedence over URL query string values
// in r.Form.
//
// If the request Body's size has not already been limited by MaxBytesReader,
// the size is capped at 10MB.
//
// ParseMultipartForm calls ParseForm automatically.
// It is idempotent.
func (r *Request) ParseForm() error {
  var err error
  if r.PostForm == nil {
    if r.Method == "POST" || r.Method == "PUT" {
      r.PostForm, err = parsePostForm(r)
    }
    if r.PostForm == nil {
      r.PostForm = make(url.Values)
    }
  }
  if r.Form == nil {
    if len(r.PostForm) > 0 {
      r.Form = make(url.Values)
      copyValues(r.Form, r.PostForm)
    }
    var newValues url.Values
    if r.URL != nil {
      var e error
      newValues, e = url.ParseQuery(r.URL.RawQuery)
      if err == nil {
        err = e
      }
    }
    if newValues == nil {
      newValues = make(url.Values)
    }
    if r.Form == nil {
      r.Form = newValues
    } else {
      copyValues(r.Form, newValues)
    }
  }
  return err
}

// ParseMultipartForm parses a request body as multipart/form-data.
// The whole request body is parsed and up to a total of maxMemory bytes of
// its file parts are stored in memory, with the remainder stored on
// disk in temporary files.
// ParseMultipartForm calls ParseForm if necessary.
// After one call to ParseMultipartForm, subsequent calls have no effect.
func (r *Request) ParseMultipartForm(maxMemory int64) error {
  if r.MultipartForm == multipartByReader {
    return errors.New("http: multipart handled by MultipartReader")
  }
  if r.Form == nil {
    err := r.ParseForm()
    if err != nil {
      return err
    }
  }
  if r.MultipartForm != nil {
    return nil
  }

  mr, err := r.multipartReader()
  if err == ErrNotMultipart {
    return nil
  } else if err != nil {
    return err
  }

  f, err := mr.ReadForm(maxMemory)
  if err != nil {
    return err
  }
  for k, v := range f.Value {
    r.Form[k] = append(r.Form[k], v...)
  }
  r.MultipartForm = f

  return nil
}

// FormValue returns the first value for the named component of the query.
// POST and PUT body parameters take precedence over URL query string values.
// FormValue calls ParseMultipartForm and ParseForm if necessary.
// To access multiple values of the same key use ParseForm.
func (r *Request) FormValue(key string) string {
  if r.Form == nil {
    r.ParseMultipartForm(defaultMaxMemory)
  }
  if vs := r.Form[key]; len(vs) > 0 {
    return vs[0]
  }
  return ""
}

// PostFormValue returns the first value for the named component of the POST
// or PUT request body. URL query parameters are ignored.
// PostFormValue calls ParseMultipartForm and ParseForm if necessary.
func (r *Request) PostFormValue(key string) string {
  if r.PostForm == nil {
    r.ParseMultipartForm(defaultMaxMemory)
  }
  if vs := r.PostForm[key]; len(vs) > 0 {
    return vs[0]
  }
  return ""
}

// FormFile returns the first file for the provided form key.
// FormFile calls ParseMultipartForm and ParseForm if necessary.
func (r *Request) FormFile(key string) (multipart.File, *multipart.FileHeader, error) {
  if r.MultipartForm == multipartByReader {
    return nil, nil, errors.New("http: multipart handled by MultipartReader")
  }
  if r.MultipartForm == nil {
    err := r.ParseMultipartForm(defaultMaxMemory)
    if err != nil {
      return nil, nil, err
    }
  }
  if r.MultipartForm != nil && r.MultipartForm.File != nil {
    if fhs := r.MultipartForm.File[key]; len(fhs) > 0 {
      f, err := fhs[0].Open()
      return f, fhs[0], err
    }
  }
  return nil, nil, ErrMissingFile
}

func (r *Request) expectsContinue() bool {
  return hasToken(r.Header.get("Expect"), "100-continue")
}
