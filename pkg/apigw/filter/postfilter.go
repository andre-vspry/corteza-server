package filter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	atypes "github.com/cortezaproject/corteza-server/automation/types"
	agctx "github.com/cortezaproject/corteza-server/pkg/apigw/ctx"
	"github.com/cortezaproject/corteza-server/pkg/apigw/types"
	pe "github.com/cortezaproject/corteza-server/pkg/errors"
	"github.com/cortezaproject/corteza-server/pkg/expr"
)

type (
	redirection struct {
		types.FilterMeta

		location *url.URL
		status   int

		params struct {
			HTTPStatus int    `json:"status,string"`
			Location   string `json:"location"`
		}
	}

	// support for arbitrary response
	// obfuscation
	customResponse struct {
		types.FilterMeta
		params struct {
			Source string `json:"source"`
		}
	}

	jsonResponse struct {
		types.FilterMeta

		params struct {
			Exp       *atypes.Expr
			Evaluable expr.Evaluable
		}
	}

	defaultJsonResponse struct {
		types.FilterMeta
	}
)

func NewRedirection() (e *redirection) {
	e = &redirection{}

	e.Name = "redirection"
	e.Label = "Redirection"
	e.Kind = types.PostFilter

	e.Args = []*types.FilterMetaArg{
		{
			Type:    "status",
			Label:   "status",
			Options: map[string]interface{}{},
		},
		{
			Type:    "text",
			Label:   "location",
			Options: map[string]interface{}{},
		},
	}

	return
}

func (h redirection) New() types.Handler {
	return NewRedirection()
}

func (h redirection) String() string {
	return fmt.Sprintf("apigw filter %s (%s)", h.Name, h.Label)
}

func (h redirection) Meta() types.FilterMeta {
	return h.FilterMeta
}

func (h redirection) Weight() int {
	return h.Wgt
}

func (h *redirection) Merge(params []byte) (types.Handler, error) {
	err := json.NewDecoder(bytes.NewBuffer(params)).Decode(&h.params)

	loc, err := url.ParseRequestURI(h.params.Location)

	if err != nil {
		return nil, fmt.Errorf("could not validate parameters, invalid URL: %s", err)
	}

	if !checkStatus("redirect", h.params.HTTPStatus) {
		return nil, fmt.Errorf("could not validate parameters, wrong status %d", h.params.HTTPStatus)
	}

	h.location = loc
	h.status = h.params.HTTPStatus

	return h, err
}

func (h redirection) Handler() types.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) error {
		http.Redirect(rw, r, h.location.String(), h.status)
		return nil
	}
}

func NewDefaultJsonResponse() (e *defaultJsonResponse) {
	e = &defaultJsonResponse{}

	e.Name = "defaultJsonResponse"
	e.Label = "Default JSON response"
	e.Kind = types.PostFilter

	return
}

func (j defaultJsonResponse) New() types.Handler {
	return NewDefaultJsonResponse()
}

func (j defaultJsonResponse) String() string {
	return fmt.Sprintf("apigw filter %s (%s)", j.Name, j.Label)
}

func (j defaultJsonResponse) Meta() types.FilterMeta {
	return j.FilterMeta
}

func (j *defaultJsonResponse) Merge(params []byte) (h types.Handler, err error) {
	return j, err
}

func (j defaultJsonResponse) Handler() types.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) error {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusAccepted)

		if _, err := rw.Write([]byte(`{}`)); err != nil {
			return pe.Internal("could not write to body: %v", err)
		}

		return nil
	}
}

func checkStatus(typ string, status int) bool {
	switch typ {
	case "redirect":
		return status >= 300 && status <= 399
	default:
		return true
	}
}

func NewJsonResponse() (e *jsonResponse) {
	e = &jsonResponse{}

	e.Name = "jsonResponse"
	e.Label = "JSON response"
	e.Kind = types.PostFilter

	e.Args = []*types.FilterMetaArg{
		{
			Type:    "input",
			Label:   "input",
			Options: map[string]interface{}{},
		},
	}

	return
}

func (j jsonResponse) New() types.Handler {
	return NewJsonResponse()
}

func (j jsonResponse) String() string {
	return fmt.Sprintf("apigw filter %s (%s)", j.Name, j.Label)
}

func (j jsonResponse) Meta() types.FilterMeta {
	return j.FilterMeta
}

func (j *jsonResponse) Merge(params []byte) (h types.Handler, err error) {
	var (
		parser = expr.NewParser()
	)

	err = json.NewDecoder(bytes.NewBuffer(params)).Decode(&j.params.Exp)

	if err != nil {
		return j, err
	}

	j.params.Evaluable, err = parser.Parse(j.params.Exp.Expr)

	if err != nil {
		return j, fmt.Errorf("could not evaluate expression: %s", err)
	}

	return j, err
}

func (j jsonResponse) Handler() types.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) (err error) {
		var (
			ctx   = r.Context()
			scope = agctx.ScopeFromContext(ctx)

			evald *expr.Vars
			body  expr.TypedValue
		)

		// what happens with non-string values?

		scope.Set("body", "")

		in, err := expr.NewVars(scope.Export())

		if err != nil {
			return pe.Internal("could not validate request data: %v", err)
		}

		j.params.Exp.SetType(func(s string) (expr.Type, error) { return expr.String{}, nil })
		j.params.Exp.SetEval(j.params.Evaluable)

		set := atypes.ExprSet{j.params.Exp}

		evald, err = set.Eval(ctx, in)

		if err != nil {
			return
		}

		body, err = evald.Select("body")

		if err != nil {
			return
		}

		// payload, err := scope.Get(j.params.Input)
		// a := payload.(*expr.Array).Get()
		// // spew.Dump("VAL", payload, err, scope.Keys(), a)
		// // spew.Dump(a.([]expr.TypedValue)[0].Get())
		// a = []*ctypes.Record{a.([]expr.TypedValue)[0].Get().(*ctypes.Record)}

		rw.Header().Add("Content-Type", "application/json")
		rw.Write([]byte(body.Get().(string)))

		// e := json.NewEncoder(rw)
		// e.Encode()

		return nil
	}
}
