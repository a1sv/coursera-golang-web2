package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

type wTpl struct {
	FuncName      string
	RecieverName  string
	API           apiComment
	ParamToReturn string
}

type sTpl struct {
	StructName  string
	GenComments map[string]apiComment
}

type vTpl struct {
	Name   string
	Struct *structToValidate
}

type parsedReceivers struct {
	methodName  []string
	structName  map[string]bool
	genComments map[string]apiComment
}

type parsed struct {
	name              string
	recieverName      string
	comment           apiComment
	funcParamToReturn string
}

type apiComment struct {
	StructName   string `json:"-"`
	FuncName     string `json:"-"`
	RecieverName string `json:"-"`
	URL          string
	Auth         bool
	Method       string
}

type structToValidate struct {
	Name   string
	Fields []structToValidateField
}

type structToValidateField struct {
	Name  string
	Rules apiValidationRules
	Type  string
}

type apiValidationRules struct {
	Required  bool
	ParamName string
	Enum      []string
	Default   string
	Min       bool
	MinVal    int
	Max       bool
	MaxVal    int
}

var wrapperTpl = template.Must(template.New("wrapperTpl").Parse(`
func (h *{{.RecieverName}})wrapper{{.FuncName}} (w http.ResponseWriter, r *http.Request) (interface{}, error) {
	{{if .API.Auth -}}
	if r.Header.Get("X-Auth") != "100500" {
		return nil, ApiError{HTTPStatus: http.StatusForbidden, Err: fmt.Errorf("unauthorized")}
	}
	{{end -}}
	{{if .API.Method -}}
	if r.Method != "{{ .API.Method }}" {
		return nil, ApiError{HTTPStatus: http.StatusNotAcceptable, Err: fmt.Errorf("bad method")}
	}
	{{end}}
	var params url.Values
	if r.Method == "GET" {
		params = r.URL.Query()
	} else {
		body, _ := ioutil.ReadAll(r.Body)
		params, _ = url.ParseQuery(string(body))
	}
	in, err := validate{{ .ParamToReturn }}(params)
	if err != nil {
		return nil, err
	}
	return h.{{ .FuncName }}(r.Context(), in)
}
`))

var validationTpl = template.Must(template.New("validationTpl").Parse(`
 func validate{{.Name}}(v url.Values) ({{.Name }}, error){
	var err error
	s := {{ .Name }}{}
	{{- range $k, $f := .Struct.Fields}}
	{{- if eq $f.Type "Int" }}
	s.{{ .Name }}, err = strconv.Atoi(v.Get("{{ $f.Rules.ParamName }}"))
	if err != nil {
		return s, ApiError{http.StatusBadRequest, fmt.Errorf("{{ $f.Rules.ParamName }} must me int")}
	}

	{{ else }}
	s.{{ .Name }} = v.Get("{{ $f.Rules.ParamName }}")

	{{ end -}}

	{{- if $f.Rules.Default -}}
	if s.{{ .Name }} == "" {
		s.{{ .Name }} = "{{ $f.Rules.Default }}"
	}

	{{ end -}}

	{{- if $f.Rules.Required -}}
	if s.{{ .Name }} == "" {
		return s, ApiError{http.StatusBadRequest, fmt.Errorf("{{ $f.Rules.ParamName }} must me not empty")}
	}

	{{ end -}}

	{{- if and $f.Rules.Min (eq .Type "Int") -}}
	if s.{{ .Name }} < {{ $f.Rules.MinVal }} {
		return s, ApiError{http.StatusBadRequest, fmt.Errorf("{{ $f.Rules.ParamName }} must be >= {{ $f.Rules.MinVal }}")}
	}

	{{ end -}}

	{{ if and $f.Rules.Min (eq .Type "String") -}}
	if len(s.{{ .Name }}) < {{ $f.Rules.MinVal }} {
		return s, ApiError{http.StatusBadRequest, fmt.Errorf("{{ $f.Rules.ParamName }} len must be >= {{ $f.Rules.MinVal }}")}
	}

	{{ end -}}

	{{- if and $f.Rules.Max (eq .Type "Int") -}}
	if s.{{ .Name }} > {{ $f.Rules.MaxVal }} {
		return s, ApiError{http.StatusBadRequest, fmt.Errorf("{{ $f.Rules.ParamName }} must be <= {{ $f.Rules.MaxVal }}")}
	}

	{{ end -}}

	{{- if and $f.Rules.Max (eq .Type "String") -}}
	if len(s.{{ .Name }}) > {{ $f.Rules.MaxVal }} {
		return s, ApiError{http.StatusBadRequest, fmt.Errorf("{{ $f.Rules.ParamName }} len must be <= {{ $f.Rules.MaxVal }}")}
	}

	{{ end -}}

	{{- if $f.Rules.Enum -}}
	enum{{ .Name }}Valid := false
	enum{{ .Name }} := []string{ {{- range $index, $element := $f.Rules.Enum }}{{ if $index }}, {{ end }}"{{ $element }}"{{ end -}} }

	for _, valid := range enum{{ .Name }} {
		if valid == s.{{ .Name }} {
			enum{{ .Name }}Valid = true
			break
		}
	}

	if !enum{{ .Name }}Valid {
		return s, ApiError{http.StatusBadRequest, fmt.Errorf("{{ $f.Rules.ParamName }} must be one of [%s]", strings.Join(enum{{ .Name }}, ", "))}
	}

	{{ end -}}

	{{- end -}}
	return s, err
 }
`))

var serveTpl = template.Must(template.New("sTpl").Parse(`
func (h *{{.StructName}}) ServeHTTP (w http.ResponseWriter, r *http.Request) () {
	var (
		err error
		out interface{}
	)

	switch r.URL.Path {
	     {{$strName := .StructName -}}
	     {{range $k ,$v := .GenComments -}}
	     {{if eq $v.RecieverName $strName -}}
	     case "{{$v.URL}}":
		   out, err = h.wrapper{{ $v.FuncName }}(w, r)
		{{end -}}
		{{end -}}default:
		   err = ApiError{Err: fmt.Errorf("unknown method"), HTTPStatus: http.StatusNotFound}
	}

	res := struct {
		Payload  interface{} ` + "`" + `json:"response,omitempty"` + "`" + `
		Error string      ` + "`" + `json:"error"` + "`" + `
	}{}

	if err == nil {
		res.Payload = out
	} else {
		res.Error = err.Error()
		if errApi, ok := err.(ApiError); ok {
			w.WriteHeader(errApi.HTTPStatus)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	jsonResponse, _ := json.Marshal(res)
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
}
`))

func generateFileHeader(nodename string) string {
	packagestr := "package " + nodename
	return packagestr + `
// GENERATED BY codegen.go, DO NOT EDIT MANUALLY
	
import (
		"fmt"
		"net/http"
		"net/url"
		"encoding/json"
		"io/ioutil"
		"strconv"
		"strings"
)
	`
}

// write the AST, pretty-printed with the go/printer package, Package printer implements printing of AST nodes.
// type Node interface {
//     Pos() token.Pos // position of first character belonging to the node
//     End() token.Pos // position of first character immediately after the node
// }

func getFuncArgNames(f *ast.FuncDecl, fset *token.FileSet) string {
	var buf bytes.Buffer
	paramType := f.Type.Params.List[1].Type
	printer.Fprint(&buf, fset, paramType)

	return buf.String()
}

func getFuncReciever(f *ast.FuncDecl) string {
	if f.Recv != nil {
		for _, rcv := range f.Recv.List {
			if rcv, ok := rcv.Type.(*ast.StarExpr); ok {
				if rcv, ok := rcv.X.(*ast.Ident); ok {
					return rcv.Name
				}
			}

			if rcv, ok := rcv.Type.(*ast.Ident); ok {
				return rcv.Name
			}
		}
	}

	return ""
}

func (p *parsedReceivers) getFuncDocAPIComment(f *ast.FuncDecl) apiComment {
	reciever := getFuncReciever(f)
	ac := &apiComment{}
	for _, comment := range f.Doc.List {
		ct := comment.Text
		j := ct[len("// apigen:api"):]
		if err := json.Unmarshal([]byte(j), ac); err != nil {
			log.Fatalf("unmarshalling error at getfuncdocAPIComment: %v, comment: %v", err, j)
		}
		ac.FuncName = f.Name.Name
		ac.RecieverName = reciever
		p.genComments[reciever+"_"+f.Name.Name] = *ac
	}
	return *ac
}

func main() {
	parsedMap := []parsed{}
	// svm := []structToValidate{}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, os.Args[1], nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	out, _ := os.Create(os.Args[2])
	in, _ := os.Open(os.Args[1])
	defer in.Close()

	fmt.Fprintln(out, generateFileHeader(node.Name.Name))
	ps := &parsedReceivers{structName: make(map[string]bool), genComments: make(map[string]apiComment)}
	sv := make(map[string]*structToValidate)

METHODS_LOOP:
	for _, f := range node.Decls {
		fd, ok := f.(*ast.FuncDecl)
		if !ok {
			fmt.Printf("SKIP %T is not *ast.FuncDecl\n", f)
			continue
		}

		if fd.Recv == nil {
			fmt.Printf("Function %v is not a method \n", fd.Name)
			continue
		}
		if fd.Doc == nil {
			fmt.Printf("SKIP method %#v doesnt have comment\n", fd.Name)
			continue
		}
		needCodegen := false
		for _, comment := range fd.Doc.List {
			apigenprefix := " apigen:api"
			needCodegen = needCodegen || strings.Contains(comment.Text, apigenprefix)
		}
		if !needCodegen {
			fmt.Printf("SKIP method %#v doesnt have apigen mark\n", fd.Name)
			continue METHODS_LOOP
		} else {
			ps.getFuncDocAPIComment(fd)
		}
		recvStruct := getFuncReciever(fd)

		parsedMap = append(parsedMap, parsed{name: fd.Name.Name, recieverName: getFuncReciever(fd), comment: ps.getFuncDocAPIComment(fd),
			funcParamToReturn: getFuncArgNames(fd, fset)})

		ps.structName[recvStruct] = true
		ps.methodName = append(ps.methodName, fd.Name.Name)

	}

	for _, d := range node.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			fmt.Printf("SKIP %T is not *ast.GenDecl\n", gd)
			continue
		}
		//	STRUCT_LOOP:
		for _, spec := range gd.Specs {
			currType, ok := spec.(*ast.TypeSpec)
			if !ok {
				fmt.Printf("SKIP %T is not ast.TypeSpec\n", spec)
				continue
			}
			currStruct, ok := currType.Type.(*ast.StructType)
			if !ok {
				fmt.Printf("SKIP %T is not ast.StructType\n", currStruct)
				continue
			}

			// FIELDS_LOOP:
			for _, field := range currStruct.Fields.List {

				if field.Tag != nil {
					tags := field.Tag.Value
					genValidation := regexp.MustCompile("`apivalidator:\"(.*)\"`").FindStringSubmatch(tags)
					if len(genValidation) > 0 {

						if _, exists := sv[currType.Name.Name]; !exists {
							sv[currType.Name.Name] = &structToValidate{
								Name: currType.Name.Name,
							}
						}

						fmt.Printf("STRUCT TAG VALUE %v\n", currType.Name.Name)

						svf := structToValidateField{Name: field.Names[0].Name, Type: strings.Title(field.Type.(*ast.Ident).Name)}
						rules := apiValidationRules{ParamName: strings.ToLower(field.Names[0].Name)}

						for _, rule := range strings.Split(genValidation[1], ",") {
							ruleParts := strings.Split(rule, "=")
							switch ruleParts[0] {
							case "required":
								rules.Required = true
							case "paramname":
								rules.ParamName = ruleParts[1]
							case "min":
								rules.Min = true
								rules.MinVal, _ = strconv.Atoi(ruleParts[1])
							case "max":
								rules.Max = true
								rules.MaxVal, _ = strconv.Atoi(ruleParts[1])
							case "enum":
								rules.Enum = strings.Split(ruleParts[1], "|")
							case "default":
								rules.Default = ruleParts[1]
							}
						}
						svf.Rules = rules
						sv[currType.Name.Name].Fields = append(sv[currType.Name.Name].Fields, svf)
						fmt.Printf("~~~~~~~~~~~~~ %v \n", sv[currType.Name.Name].Fields)

					}
				}
			}
		}

	}
	fmt.Printf("PARSED parsedReceivers: %v\n", ps)

	// Generating ServeHTTP methods
	for k := range ps.structName {

		if err := serveTpl.Execute(out, sTpl{k, ps.genComments}); err != nil {
			log.Fatal(err)
		}
	}
	for _, v := range parsedMap {
		// Generating Wrappers
		if err := wrapperTpl.Execute(out, wTpl{v.name, v.recieverName, v.comment, v.funcParamToReturn}); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Generating Api method %#v\n", v)
	}
	for n, v := range sv {
		fmt.Printf("$$$$$$ %v:%v\n", n, v)
		if err := validationTpl.Execute(out, vTpl{n, v}); err != nil {
			log.Fatal(err)
		}
	}
}
