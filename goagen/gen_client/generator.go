package genclient

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/goadesign/goa/design"
	"github.com/goadesign/goa/goagen/codegen"
	"github.com/goadesign/goa/goagen/gen_app"
	"github.com/goadesign/goa/goagen/utils"
)

// Filename used to generate all data types (without the ".go" extension)
const typesFileName = "datatypes"

// Generator is the application code generator.
type Generator struct {
	outDir         string // Path to output directory
	genfiles       []string
	generatedTypes map[string]bool // Keeps track of names of user types that correspond to action payloads.
	encoders       []*genapp.EncoderTemplateData
	decoders       []*genapp.EncoderTemplateData
	encoderImports []string
}

// Generate is the generator entry point called by the meta generator.
func Generate() (files []string, err error) {
	var outDir string

	set := flag.NewFlagSet("client", flag.PanicOnError)
	set.String("design", "", "")
	set.StringVar(&outDir, "out", "", "")
	set.Parse(os.Args[2:])

	g := &Generator{outDir: outDir}

	return g.Generate(design.Design)
}

// Generate generats the client package and CLI.
func (g *Generator) Generate(api *design.APIDefinition) (_ []string, err error) {
	go utils.Catch(nil, func() { g.Cleanup() })

	defer func() {
		if err != nil {
			g.Cleanup()
		}
	}()

	// Make tool directory
	var toolDir string
	toolDir, err = g.makeToolDir(api.Name)
	if err != nil {
		return
	}

	// Setup generation
	funcs := template.FuncMap{
		"add":             func(a, b int) int { return a + b },
		"cmdFieldType":    cmdFieldType,
		"defaultPath":     defaultPath,
		"escapeBackticks": escapeBackticks,
		"flagType":        flagType,
		"goify":           codegen.Goify,
		"gotypedef":       codegen.GoTypeDef,
		"gotypedesc":      codegen.GoTypeDesc,
		"gotyperef":       codegen.GoTypeRef,
		"gotypename":      codegen.GoTypeName,
		"gotyperefext":    goTypeRefExt,
		"join":            join,
		"joinStrings":     strings.Join,
		"multiComment":    multiComment,
		"pathParams":      pathParams,
		"pathParamNames":  pathParamNames,
		"pathTemplate":    pathTemplate,
		"tempvar":         codegen.Tempvar,
		"title":           strings.Title,
		"toString":        toString,
		"typeName":        typeName,
		"signerType":      signerType,
	}
	clientPkg, err := codegen.PackagePath(g.outDir)
	if err != nil {
		return
	}
	arrayToStringTmpl = template.Must(template.New("client").Funcs(funcs).Parse(arrayToStringT))

	// Generate client/client-cli/main.go
	if err = g.generateMain(filepath.Join(toolDir, "main.go"), clientPkg, funcs, api); err != nil {
		return
	}

	// Generate client/client-cli/commands.go
	if err = g.generateCommands(filepath.Join(toolDir, "commands.go"), clientPkg, funcs, api); err != nil {
		return
	}

	// Generate client/client.go
	if err = g.generateClient(filepath.Join(g.outDir, "client.go"), clientPkg, funcs, api); err != nil {
		return
	}

	// Generate client/$res.go and types.go
	if err = g.generateClientResources(clientPkg, funcs, api); err != nil {
		return
	}

	return g.genfiles, nil
}

// Cleanup removes all the files generated by this generator during the last invokation of Generate.
func (g *Generator) Cleanup() {
	for _, f := range g.genfiles {
		os.Remove(f)
	}
	g.genfiles = nil
}

func (g *Generator) generateClient(clientFile string, clientPkg string, funcs template.FuncMap, api *design.APIDefinition) error {
	file, err := codegen.SourceFileFor(clientFile)
	if err != nil {
		return err
	}
	clientTmpl := template.Must(template.New("client").Funcs(funcs).Parse(clientTmpl))

	// Compute list of encoders and decoders
	encoders, err := genapp.BuildEncoders(api.Produces, true)
	if err != nil {
		return err
	}
	decoders, err := genapp.BuildEncoders(api.Consumes, false)
	if err != nil {
		return err
	}
	im := make(map[string]bool)
	for _, data := range encoders {
		im[data.PackagePath] = true
	}
	for _, data := range decoders {
		im[data.PackagePath] = true
	}
	var packagePaths []string
	for packagePath := range im {
		if packagePath != "github.com/goadesign/goa" {
			packagePaths = append(packagePaths, packagePath)
		}
	}
	sort.Strings(packagePaths)

	// Setup codegen
	imports := []*codegen.ImportSpec{
		codegen.SimpleImport("net/http"),
		codegen.SimpleImport("github.com/goadesign/goa"),
		codegen.NewImport("goaclient", "github.com/goadesign/goa/client"),
	}
	for _, packagePath := range packagePaths {
		imports = append(imports, codegen.SimpleImport(packagePath))
	}
	if err := file.WriteHeader("", "client", imports); err != nil {
		return err
	}
	g.genfiles = append(g.genfiles, clientFile)

	// Generate
	data := struct {
		API      *design.APIDefinition
		Encoders []*genapp.EncoderTemplateData
		Decoders []*genapp.EncoderTemplateData
	}{
		API:      api,
		Encoders: encoders,
		Decoders: decoders,
	}
	if err := clientTmpl.Execute(file, data); err != nil {
		return err
	}

	return file.FormatCode()
}

func (g *Generator) generateClientResources(clientPkg string, funcs template.FuncMap, api *design.APIDefinition) error {
	userTypeTmpl := template.Must(template.New("userType").Funcs(funcs).Parse(userTypeTmpl))
	typeDecodeTmpl := template.Must(template.New("typeDecode").Funcs(funcs).Parse(typeDecodeTmpl))

	err := api.IterateResources(func(res *design.ResourceDefinition) error {
		return g.generateResourceClient(res, funcs)
	})
	if err != nil {
		return err
	}
	types := make(map[string]*design.UserTypeDefinition)
	for _, res := range api.Resources {
		for n, ut := range res.UserTypes() {
			types[n] = ut
		}
	}
	filename := filepath.Join(g.outDir, typesFileName+".go")
	file, err := codegen.SourceFileFor(filename)
	if err != nil {
		return err
	}
	imports := []*codegen.ImportSpec{
		codegen.SimpleImport("github.com/goadesign/goa"),
		codegen.SimpleImport("fmt"),
		codegen.SimpleImport("io"),
		codegen.SimpleImport("net/http"),
		codegen.SimpleImport("time"),
		codegen.NewImport("uuid", "github.com/goadesign/goa/uuid"),
	}
	if err := file.WriteHeader("User Types", "client", imports); err != nil {
		return err
	}
	g.genfiles = append(g.genfiles, filename)

	// Generate user and media types used by action payloads and parameters
	err = api.IterateUserTypes(func(userType *design.UserTypeDefinition) error {
		if _, ok := g.generatedTypes[userType.TypeName]; ok {
			return nil
		}
		if _, ok := types[userType.TypeName]; ok {
			g.generatedTypes[userType.TypeName] = true
			return userTypeTmpl.Execute(file, userType)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Generate media types used by action responses and their load helpers
	err = api.IterateResources(func(res *design.ResourceDefinition) error {
		return res.IterateActions(func(a *design.ActionDefinition) error {
			return a.IterateResponses(func(r *design.ResponseDefinition) error {
				if mt := api.MediaTypeWithIdentifier(r.MediaType); mt != nil {
					if _, ok := g.generatedTypes[mt.TypeName]; !ok {
						g.generatedTypes[mt.TypeName] = true
						if !mt.IsBuiltIn() {
							if err := userTypeTmpl.Execute(file, mt); err != nil {
								return err
							}
						}
						typeName := mt.TypeName
						if mt.IsBuiltIn() {
							elems := strings.Split(typeName, ".")
							typeName = elems[len(elems)-1]
						}
						if err := typeDecodeTmpl.Execute(file, mt); err != nil {
							return err
						}
					}
				}
				return nil
			})
		})
	})
	if err != nil {
		return err
	}

	// Generate media types used in payloads but not in responses
	err = api.IterateMediaTypes(func(mediaType *design.MediaTypeDefinition) error {
		if mediaType.IsBuiltIn() {
			return nil
		}
		if _, ok := g.generatedTypes[mediaType.TypeName]; ok {
			return nil
		}
		if _, ok := types[mediaType.TypeName]; ok {
			g.generatedTypes[mediaType.TypeName] = true
			return userTypeTmpl.Execute(file, mediaType)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return file.FormatCode()
}

func (g *Generator) generateResourceClient(res *design.ResourceDefinition, funcs template.FuncMap) error {
	payloadTmpl := template.Must(template.New("payload").Funcs(funcs).Parse(payloadTmpl))
	pathTmpl := template.Must(template.New("pathTemplate").Funcs(funcs).Parse(pathTmpl))

	resFilename := codegen.SnakeCase(res.Name)
	if resFilename == typesFileName {
		// Avoid clash with datatypes.go
		resFilename += "_client"
	}
	filename := filepath.Join(g.outDir, resFilename+".go")
	file, err := codegen.SourceFileFor(filename)
	if err != nil {
		return err
	}
	imports := []*codegen.ImportSpec{
		codegen.SimpleImport("bytes"),
		codegen.SimpleImport("encoding/json"),
		codegen.SimpleImport("fmt"),
		codegen.SimpleImport("io"),
		codegen.SimpleImport("net/http"),
		codegen.SimpleImport("net/url"),
		codegen.SimpleImport("strconv"),
		codegen.SimpleImport("strings"),
		codegen.SimpleImport("time"),
		codegen.SimpleImport("golang.org/x/net/context"),
		codegen.SimpleImport("golang.org/x/net/websocket"),
		codegen.NewImport("uuid", "github.com/goadesign/goa/uuid"),
	}
	if err := file.WriteHeader("", "client", imports); err != nil {
		return err
	}
	g.genfiles = append(g.genfiles, filename)
	g.generatedTypes = make(map[string]bool)
	err = res.IterateActions(func(action *design.ActionDefinition) error {
		if action.Payload != nil {
			if err := payloadTmpl.Execute(file, action); err != nil {
				return err
			}
			g.generatedTypes[action.Payload.TypeName] = true
		}
		if action.Params != nil {
			params := make(design.Object, len(action.QueryParams.Type.ToObject()))
			for n, param := range action.QueryParams.Type.ToObject() {
				name := codegen.Goify(n, false)
				params[name] = param
			}
			action.QueryParams.Type = params
		}
		if action.Headers != nil {
			headers := make(design.Object, len(action.Headers.Type.ToObject()))
			for n, header := range action.Headers.Type.ToObject() {
				name := codegen.Goify(n, false)
				headers[name] = header
			}
			action.Headers.Type = headers
		}
		for i, r := range action.Routes {
			data := struct {
				Route *design.RouteDefinition
				Index int
			}{
				Route: r,
				Index: i,
			}
			if err := pathTmpl.Execute(file, data); err != nil {
				return err
			}
		}
		return g.generateActionClient(action, file, funcs)
	})
	if err != nil {
		return err
	}

	return file.FormatCode()
}

func (g *Generator) generateActionClient(action *design.ActionDefinition, file *codegen.SourceFile, funcs template.FuncMap) error {
	var (
		params        []string
		names         []string
		queryParams   []*paramData
		headers       []*paramData
		signer        string
		clientsTmpl   = template.Must(template.New("clients").Funcs(funcs).Parse(clientsTmpl))
		requestsTmpl  = template.Must(template.New("requests").Funcs(funcs).Parse(requestsTmpl))
		clientsWSTmpl = template.Must(template.New("clientsws").Funcs(funcs).Parse(clientsWSTmpl))
	)
	if action.Payload != nil {
		params = append(params, "payload "+codegen.GoTypeRef(action.Payload, action.Payload.AllRequired(), 1, false))
		names = append(names, "payload")
	}
	initParams := func(att *design.AttributeDefinition) []*paramData {
		if att == nil {
			return nil
		}
		obj := att.Type.ToObject()
		var pdata []*paramData
		var optData []*paramData
		var pnames, pparams []string
		var optNames, optParams []string
		for n, q := range obj {
			varName := codegen.Goify(n, false)
			param := &paramData{
				Name:      n,
				VarName:   varName,
				Attribute: q,
			}
			if q.Type.IsPrimitive() {
				param.MustToString = q.Type.Kind() != design.StringKind
				if att.IsRequired(n) {
					param.ValueName = varName
					pdata = append(pdata, param)
					pparams = append(pparams, varName+" "+cmdFieldType(q.Type, false))
					pnames = append(pnames, varName)
				} else {
					param.ValueName = "*" + varName
					param.CheckNil = true
					optData = append(optData, param)
					optParams = append(optParams, varName+" "+cmdFieldType(q.Type, true))
					optNames = append(optNames, varName)
				}
			} else {
				param.MustToString = true
				param.ValueName = varName
				param.CheckNil = true
				if att.IsRequired(n) {
					pdata = append(pdata, param)
					pparams = append(params, varName+" "+cmdFieldType(q.Type, false))
					pnames = append(pnames, varName)
				} else {
					optData = append(optData, param)
					optParams = append(optParams, varName+" "+cmdFieldType(q.Type, false))
					optNames = append(optNames, varName)
				}
			}
		}
		sort.Strings(pparams)
		sort.Strings(optParams)
		sort.Strings(pnames)
		sort.Strings(optNames)

		// Update closure
		names = append(names, pnames...)
		names = append(names, optNames...)
		params = append(params, pparams...)
		params = append(params, optParams...)

		sort.Sort(byParamName(pdata))
		sort.Sort(byParamName(optData))
		return append(pdata, optData...)
	}
	queryParams = initParams(action.QueryParams)
	headers = initParams(action.Headers)
	if action.Security != nil {
		signer = codegen.Goify(action.Security.Scheme.SchemeName, true)
	}
	data := struct {
		Name            string
		ResourceName    string
		Description     string
		Routes          []*design.RouteDefinition
		HasPayload      bool
		Params          string
		ParamNames      string
		CanonicalScheme string
		Signer          string
		QueryParams     []*paramData
		Headers         []*paramData
	}{
		Name:            action.Name,
		ResourceName:    action.Parent.Name,
		Description:     action.Description,
		Routes:          action.Routes,
		HasPayload:      action.Payload != nil,
		Params:          strings.Join(params, ", "),
		ParamNames:      strings.Join(names, ", "),
		CanonicalScheme: action.CanonicalScheme(),
		Signer:          signer,
		QueryParams:     queryParams,
		Headers:         headers,
	}
	if action.WebSocket() {
		return clientsWSTmpl.Execute(file, data)
	}
	if err := clientsTmpl.Execute(file, data); err != nil {
		return err
	}
	return requestsTmpl.Execute(file, data)
}

// join is a code generation helper function that generates a function signature built from
// concatenating the properties (name type) of the given attribute type (assuming it's an object).
// join accepts an optional slice of strings which indicates the order in which the parameters
// should appear in the signature. If pos is specified then it must list all the parameters. If
// it's not specified then parameters are sorted alphabetically.
func join(att *design.AttributeDefinition, usePointers bool, pos ...[]string) string {
	if att == nil {
		return ""
	}
	obj := att.Type.ToObject()
	elems := make([]string, len(obj))
	var keys []string
	if len(pos) > 0 {
		keys = pos[0]
		if len(keys) != len(obj) {
			panic("invalid position slice, lenght does not match attribute field count") // bug
		}
	} else {
		keys = make([]string, len(obj))
		i := 0
		for n := range obj {
			keys[i] = n
			i++
		}
		sort.Strings(keys)
	}
	for i, n := range keys {
		a := obj[n]
		elems[i] = fmt.Sprintf("%s %s", codegen.Goify(n, false), cmdFieldType(a.Type, usePointers && !a.IsRequired(n)))
	}
	return strings.Join(elems, ", ")
}

// escapeBackticks is a code generation helper that escapes backticks in a string.
func escapeBackticks(text string) string {
	return strings.Replace(text, "`", "`+\"`\"+`", -1)
}

// multiComment produces a Go comment containing the given string taking into account newlines.
func multiComment(text string) string {
	lines := strings.Split(text, "\n")
	nl := make([]string, len(lines))
	for i, l := range lines {
		nl[i] = "// " + strings.TrimSpace(l)
	}
	return strings.Join(nl, "\n")
}

// gotTypeRefExt computes the type reference for a type in a different package.
func goTypeRefExt(t design.DataType, tabs int, pkg string) string {
	ref := codegen.GoTypeRef(t, nil, tabs, false)
	if strings.HasPrefix(ref, "*") {
		return fmt.Sprintf("%s.%s", pkg, ref[1:])
	}
	return fmt.Sprintf("%s.%s", pkg, ref)
}

// cmdFieldType computes the Go type name used to store command flags of the given design type.
func cmdFieldType(t design.DataType, point bool) string {
	var pointer, suffix string
	if point && !t.IsArray() {
		pointer = "*"
	}
	if t.Kind() == design.DateTimeKind || t.Kind() == design.UUIDKind {
		suffix = "string"
	} else {
		suffix = codegen.GoNativeType(t)
	}
	return pointer + suffix
}

// template used to produce code that serializes arrays of simple values into comma separated
// strings.
var arrayToStringTmpl *template.Template

// toString generates Go code that converts the given simple type attribute into a string.
func toString(name, target string, att *design.AttributeDefinition) string {
	switch actual := att.Type.(type) {
	case design.Primitive:
		switch actual.Kind() {
		case design.IntegerKind:
			return fmt.Sprintf("%s := strconv.Itoa(%s)", target, name)
		case design.BooleanKind:
			return fmt.Sprintf("%s := strconv.FormatBool(%s)", target, name)
		case design.NumberKind:
			return fmt.Sprintf("%s := strconv.FormatFloat(%s, 'f', -1, 64)", target, name)
		case design.StringKind, design.DateTimeKind, design.UUIDKind:
			return fmt.Sprintf("%s := %s", target, name)
		case design.AnyKind:
			return fmt.Sprintf("%s := fmt.Sprintf(\"%%v\", %s)", target, name)
		default:
			panic("unknown primitive type")
		}
	case *design.Array:
		data := map[string]interface{}{
			"Name":     name,
			"Target":   target,
			"ElemType": actual.ElemType,
		}
		return codegen.RunTemplate(arrayToStringTmpl, data)
	default:
		panic("cannot convert non simple type " + att.Type.Name() + " to string") // bug
	}
}

// flagType returns the flag type for the given (basic type) attribute definition.
func flagType(att *design.AttributeDefinition) string {
	switch att.Type.Kind() {
	case design.IntegerKind:
		return "Int"
	case design.NumberKind:
		return "Float64"
	case design.BooleanKind:
		return "Bool"
	case design.StringKind:
		return "String"
	case design.DateTimeKind:
		return "String"
	case design.UUIDKind:
		return "String"
	case design.AnyKind:
		return "String"
	case design.ArrayKind:
		return flagType(att.Type.(*design.Array).ElemType) + "Slice"
	case design.UserTypeKind:
		return flagType(att.Type.(*design.UserTypeDefinition).AttributeDefinition)
	case design.MediaTypeKind:
		return flagType(att.Type.(*design.MediaTypeDefinition).AttributeDefinition)
	default:
		panic("invalid flag attribute type " + att.Type.Name())
	}
}

// defaultPath returns the first route path for the given action that does not take any wildcard,
// empty string if none.
func defaultPath(action *design.ActionDefinition) string {
	for _, r := range action.Routes {
		candidate := r.FullPath()
		if !strings.ContainsRune(candidate, ':') {
			return candidate
		}
	}
	return ""
}

// signerType returns the name of the client signer used for the defined security model on the Action
func signerType(scheme *design.SecuritySchemeDefinition) string {
	switch scheme.Kind {
	case design.JWTSecurityKind:
		return "goaclient.JWTSigner" // goa client package imported under goaclient
	case design.OAuth2SecurityKind:
		return "goaclient.OAuth2Signer"
	case design.APIKeySecurityKind:
		return "goaclient.APIKeySigner"
	case design.BasicAuthSecurityKind:
		return "goaclient.BasicSigner"
	}
	return ""
}

// pathTemplate returns a fmt format suitable to build a request path to the reoute.
func pathTemplate(r *design.RouteDefinition) string {
	return design.WildcardRegex.ReplaceAllLiteralString(r.FullPath(), "/%v")
}

// pathParams return the function signature of the path factory function for the given route.
func pathParams(r *design.RouteDefinition) string {
	pnames := r.Params()
	params := make(design.Object, len(pnames))
	for _, p := range pnames {
		params[p] = r.Parent.Params.Type.ToObject()[p]
	}
	return join(&design.AttributeDefinition{Type: params}, false, pnames)
}

// pathParamNames return the names of the parameters of the path factory function for the given route.
func pathParamNames(r *design.RouteDefinition) string {
	params := r.Params()
	goified := make([]string, len(params))
	for i, p := range params {
		goified[i] = codegen.Goify(p, false)
	}
	return strings.Join(goified, ", ")
}

func typeName(mt *design.MediaTypeDefinition) string {
	name := codegen.GoTypeName(mt, mt.AllRequired(), 1, false)
	if mt.IsBuiltIn() {
		return strings.Split(name, ".")[1]
	}
	return name
}

// paramData is the data structure holding the information needed to generate query params and
// headers handling code.
type paramData struct {
	Name         string
	VarName      string
	ValueName    string
	Attribute    *design.AttributeDefinition
	MustToString bool
	CheckNil     bool
}

type byParamName []*paramData

func (b byParamName) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byParamName) Less(i, j int) bool { return b[i].Name < b[j].Name }
func (b byParamName) Len() int           { return len(b) }

const arrayToStringT = `	{{ $tmp := tempvar }}{{ $tmp }} := make([]string, len({{ .Name }}))
	for i, e := range {{ .Name }} {
		{{ $tmp2 := tempvar }}{{ toString "e" $tmp2 .ElemType }}
		{{ $tmp }}[i] = {{ $tmp2 }}
	}
	{{ .Target }} := strings.Join({{ $tmp }}, ",")`

const payloadTmpl = `// {{ gotypename .Payload nil 0 false }} is the {{ .Parent.Name }} {{ .Name }} action payload.
type {{ gotypename .Payload nil 1 false }} {{ gotypedef .Payload 0 true false }}
`

const userTypeTmpl = `// {{ gotypedesc . true }}
type {{ gotypename . .AllRequired 1 false }} {{ gotypedef . 0 true false }}
`

const typeDecodeTmpl = `{{ $typeName := typeName . }}{{ $funcName := printf "Decode%s" $typeName }}// {{ $funcName }} decodes the {{ $typeName }} instance encoded in resp body.
func (c *Client) {{ $funcName }}(resp *http.Response) ({{ gotyperef . .AllRequired 0 false }}, error) {
	var decoded {{ gotypename . .AllRequired 0 false }}
	err := c.Decoder.Decode(&decoded, resp.Body, resp.Header.Get("Content-Type"))
	return {{ if .IsObject }}&{{ end }}decoded, err
}
`

const pathTmpl = `{{ $funcName := printf "%sPath%s" (goify (printf "%s%s" .Route.Parent.Name (title .Route.Parent.Parent.Name)) true) ((or (and .Index (add .Index 1)) "") | printf "%v") }}{{/*
*/}}{{ with .Route }}// {{ $funcName }} computes a request path to the {{ .Parent.Name }} action of {{ .Parent.Parent.Name }}.
func {{ $funcName }}({{ pathParams . }}) string {
	return fmt.Sprintf("{{ pathTemplate . }}", {{ pathParamNames . }})
}
{{ end }}`

const clientsTmpl = `{{ $funcName := goify (printf "%s%s" .Name (title .ResourceName)) true }}{{ $desc := .Description }}{{/*
*/}}{{ if $desc }}{{ multiComment $desc }}{{ else }}{{/*
*/}}// {{ $funcName }} makes a request to the {{ .Name }} action endpoint of the {{ .ResourceName }} resource{{ end }}
func (c *Client) {{ $funcName }}(ctx context.Context, path string{{ if .Params}},  {{ .Params }}{{ end }}) (*http.Response, error) {
	req, err := c.New{{ $funcName }}Request(ctx, path{{ if .ParamNames }}, {{ .ParamNames }}{{ end }})
	if err != nil {
		return nil, err
	}
	return c.Client.Do(ctx, req)
}
`

const clientsWSTmpl = `{{ $funcName := goify (printf "%s%s" .Name (title .ResourceName)) true }}{{ $desc := .Description }}{{/*
*/}}{{ if $desc }}{{ multiComment $desc }}{{ else }}// {{ $funcName }} establishes a websocket connection to the {{ .Name }} action endpoint of the {{ .ResourceName }} resource{{ end }}
func (c *Client) {{ $funcName }}(ctx context.Context, path string{{ if .Params }}, {{ .Params }}{{ end }}) (*websocket.Conn, error) {
	scheme := c.Scheme
	if scheme == "" {
		scheme = "{{ .CanonicalScheme }}"
	}
	u := url.URL{Host: c.Host, Scheme: scheme, Path: path}
{{ if .QueryParams }}	values := u.Query()
{{ range .QueryParams }}{{ if .CheckNil }}	if {{ .VarName }} != nil {
	{{ end }}{{ if .MustToString}}{{ $tmp := tempvar }}	{{ toString .ValueName $tmp .Attribute }}
	values.Set("{{ .Name }}", {{ $tmp }})
{{ else }}	values.Set("{{ .Name }}", {{ .ValueName }})
{{ end }}{{ if .CheckNil }}	}
{{ end }}{{ end }}	u.RawQuery = values.Encode()
{{ end }}	return websocket.Dial(u.String(), "", u.String())
}
`

const requestsTmpl = `{{ $funcName := goify (printf "New%s%sRequest" (title .Name) (title .ResourceName)) true }}{{/*
*/}}// {{ $funcName }} create the request corresponding to the {{ .Name }} action endpoint of the {{ .ResourceName }} resource.
func (c *Client) {{ $funcName }}(ctx context.Context, path string{{ if .Params }}, {{ .Params }}{{ end }}) (*http.Request, error) {
{{ if .HasPayload }}	var body bytes.Buffer
	err := c.Encoder.Encode(payload, &body, "*/*") // Use default encoder
	if err != nil {
		return nil, fmt.Errorf("failed to encode body: %s", err)
	}
{{ end }}	scheme := c.Scheme
	if scheme == "" {
		scheme = "{{ .CanonicalScheme }}"
	}
	u := url.URL{Host: c.Host, Scheme: scheme, Path: path}
{{ if .QueryParams }}	values := u.Query()
{{ range .QueryParams }}{{ if .CheckNil }}	if {{ .VarName }} != nil {
	{{ end }}{{ if .MustToString }}{{ $tmp := tempvar }}	{{ toString .ValueName $tmp .Attribute }}
	values.Set("{{ .Name }}", {{ $tmp }})
{{ else }}	values.Set("{{ .Name }}", {{ .ValueName }})
{{ end }}{{ if .CheckNil }}	}
{{ end }}{{ end }}	u.RawQuery = values.Encode()
{{ end }}{{ if .HasPayload }}	req, err := http.NewRequest({{ $route := index .Routes 0 }}"{{ $route.Verb }}", u.String(), &body)
{{ else }}	req, err := http.NewRequest({{ $route := index .Routes 0 }}"{{ $route.Verb }}", u.String(), nil)
{{ end }}	if err != nil {
		return nil, err
	}
{{ if .Headers }}	header := req.Header
{{ range .Headers }}{{ if .CheckNil }}	if {{ .VarName }} != nil {
	{{ end }}{{ if .MustToString }}{{ $tmp := tempvar }}	{{ toString .ValueName $tmp .Attribute }}
	header.Set("{{ .Name }}", {{ $tmp }}){{ else }}
	header.Set("{{ .Name }}", {{ .ValueName }})
{{ end }}{{ if .CheckNil }}	}
{{ end }}{{ end }}{{ end }}{{ if .Signer }}	c.{{ .Signer }}Signer.Sign(ctx, req)
{{ end }}	return req, nil
}
`

const clientTmpl = `// Client is the {{ .API.Name }} service client.
type Client struct {
	*goaclient.Client{{range $security := .API.SecuritySchemes }}{{ $signer := signerType $security }}{{ if $signer }}
	{{ goify $security.SchemeName true }}Signer *{{ $signer }}{{ end }}{{ end }}
	Encoder *goa.HTTPEncoder
	Decoder *goa.HTTPDecoder
}

// New instantiates the client.
func New(c *http.Client) *Client {
	client := &Client{
		Client: goaclient.New(c),{{range $security := .API.SecuritySchemes }}{{ $signer := signerType $security }}{{ if $signer }}
		{{ goify $security.SchemeName true }}Signer: &{{ $signer }}{},{{ end }}{{ end }}
		Encoder: goa.NewHTTPEncoder(),
		Decoder: goa.NewHTTPDecoder(),
	}

{{ if .Encoders }}	// Setup encoders and decoders
{{ range .Encoders }}{{/*
*/}}	client.Encoder.Register({{ .PackageName }}.{{ .Function }}, "{{ joinStrings .MIMETypes "\", \"" }}")
{{ end }}{{ range .Decoders }}{{/*
*/}}	client.Decoder.Register({{ .PackageName }}.{{ .Function }}, "{{ joinStrings .MIMETypes "\", \"" }}")
{{ end }}

	// Setup default encoder and decoder
{{ range .Encoders }}{{ if .Default }}{{/*
*/}}	client.Encoder.Register({{ .PackageName }}.{{ .Function }}, "*/*")
{{ end }}{{ end }}{{ range .Decoders }}{{ if .Default }}{{/*
*/}}	client.Decoder.Register({{ .PackageName }}.{{ .Function }}, "*/*")
{{ end }}{{ end }}
{{ end }}	return client
}
`
