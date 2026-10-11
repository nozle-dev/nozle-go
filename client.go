package nozle

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "log"
 "net/http"
 "net/url"
 "strings"
 "time"
)

const DefaultEngineURL = "https://api.nozle.app/engine"
const DefaultCoreURL = "https://api.nozle.app/core"

type Option func(*Client)
// WithBaseURL overrides the Engine URL. For local deployments it also sets Core unless WithEventsURL is supplied.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u; if !c.coreExplicit { c.eventsURL = u } } }
// WithEventsURL overrides the Core URL.
func WithEventsURL(u string) Option { return func(c *Client) { c.eventsURL = u; c.coreExplicit = true } }
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }
func WithDebug(on bool) Option { return func(c *Client) { c.debug = on } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

type Client struct { baseURL, eventsURL, apiKey string; coreExplicit, debug bool; timeout time.Duration; httpClient *http.Client }
func New(apiKey string, opts ...Option) *Client {
 if strings.TrimSpace(apiKey)=="" { panic("nozle: apiKey must not be empty") }
 c:=&Client{baseURL:DefaultEngineURL,eventsURL:DefaultCoreURL,apiKey:apiKey,timeout:10*time.Second,httpClient:&http.Client{}}
 for _,o:=range opts { o(c) }
 c.baseURL=strings.TrimRight(c.baseURL,"/"); c.eventsURL=strings.TrimRight(c.eventsURL,"/")
 if c.httpClient==nil { c.httpClient=&http.Client{} }
 return c
}

type APIError struct { StatusCode int; Method,Path,Operation,Body string; Details any }
func (e *APIError) Error() string { return fmt.Sprintf("nozle: %s %s returned %d",e.Method,e.Path,e.StatusCode) }
func AsAPIError(err error, target **APIError) bool { return errors.As(err,target) }

type ValidationError struct { Operation, Message string }
func (e *ValidationError) Error() string { return "nozle: "+e.Operation+": "+e.Message }
type AuthenticationError struct { ValidationError }
type TransportError struct { Operation string; Err error }
func (e *TransportError) Error() string { return "nozle: "+e.Operation+": transport failed: "+e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }
func validateKey(key,op string) error { if !strings.HasPrefix(key,"sk_") { return &AuthenticationError{ValidationError{op,"secret sk_ key required"}} }; return nil }
func required(op,name,v string) error { if strings.TrimSpace(v)=="" { return &ValidationError{op,name+" is required"} }; return nil }
func idem(op,key string) error { if err:=required(op,"idempotency key",key);err!=nil{return err}; if len([]byte(key))>255 {return &ValidationError{op,"idempotency key exceeds 255 bytes"}}; return nil }
func segment(s string) string { return url.PathEscape(s) }
func customerPath(id string) string { return "/api/v1/customers/"+segment(id) }
func entityPath(customer,entity string) string { return customerPath(customer)+"/entities/"+segment(entity) }
func query(path string, params url.Values) string { if len(params)==0{return path}; return path+"?"+params.Encode() }
func (c *Client) endpoint(core bool) string { if core{return c.eventsURL};return c.baseURL }
func (c *Client) request(ctx context.Context, op, method string, core bool, path string, body, out any, key string) error {
 if key!="" { if err:=idem(op,key);err!=nil{return err} }
 req,err:=newJSONRequest(ctx,method,c.endpoint(core)+path,body);if err!=nil{return err}
 req.Header.Set("Authorization","Bearer "+c.apiKey)
 if key!="" {req.Header.Set("Idempotency-Key",key)}
 if c.debug {log.Printf("[nozle] %s %s",method,req.URL.Redacted())}
 requestCtx:=req.Context(); cancel:=func(){}; if c.timeout>0 {requestCtx,cancel=context.WithTimeout(requestCtx,c.timeout);req=req.WithContext(requestCtx)}
 defer cancel()
 resp,err:=c.httpClient.Do(req);if err!=nil{return &TransportError{op,err}};defer resp.Body.Close()
 if resp.StatusCode<200||resp.StatusCode>=300 {
  b,_:=io.ReadAll(io.LimitReader(resp.Body,4096));safe:=strings.ReplaceAll(string(b),c.apiKey,"[REDACTED]")
  var details any; if json.Unmarshal([]byte(safe),&details)==nil { details=redact(details) }
  return &APIError{StatusCode:resp.StatusCode,Method:method,Path:req.URL.Path,Operation:op,Body:safe,Details:details}
 }
 if out==nil||resp.StatusCode==204{return nil}
 b,err:=io.ReadAll(io.LimitReader(resp.Body,8<<20));if err!=nil{return err};if len(b)==0{return nil}
 if err=json.Unmarshal(b,out);err!=nil{return &APIError{StatusCode:resp.StatusCode,Method:method,Path:req.URL.Path,Operation:op,Body:"invalid JSON success response"}}
 return nil
}
func redact(v any) any { switch x:=v.(type) {case map[string]any: for k,y:=range x {n:=strings.ToLower(strings.ReplaceAll(k,"-","_"));if n=="authorization"||n=="api_key"||n=="secret"||n=="razorpay_signature"||strings.HasSuffix(n,"_token")||strings.HasSuffix(n,"_secret")||strings.HasSuffix(n,"_key") {x[k]="[REDACTED]"} else {x[k]=redact(y)}};case []any: for i:=range x{x[i]=redact(x[i])}};return v }
// do and decodeJSON are retained for existing low-level methods.
func (c *Client) do(ctx context.Context,method,u string,body any)(*http.Response,error){ req,err:=newJSONRequest(ctx,method,u,body);if err!=nil{return nil,err};req.Header.Set("Authorization","Bearer "+c.apiKey);resp,err:=c.httpClient.Do(req);if err!=nil{return nil,err};if resp.StatusCode<200||resp.StatusCode>=300 {defer resp.Body.Close();b,_:=readBody(resp);return nil,&APIError{StatusCode:resp.StatusCode,Method:method,Path:req.URL.Path,Body:string(b)}};return resp,nil }
func decodeJSON(resp *http.Response,v any)error{defer resp.Body.Close();return json.NewDecoder(resp.Body).Decode(v)}
