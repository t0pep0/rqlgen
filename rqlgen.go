package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const (
	tagRex  = `gorethink:"([a-zA-Z0-9\-\_]*)[,]?(omidempty)?[a-zA-Z0-9\-\_\,]*"`
	typeRex = `rqlgen:"(number|string|time|map_string|map_number|map_time|map_bool|map_rql|array_string|array_number|array_time|array_bool|array_rql|bool|rql)"`
)

func getFieldType(field ast.Expr) string {
	switch v := field.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		if i, ok := v.X.(*ast.Ident); ok {
			return "*" + i.Name
		} else if i, ok := v.X.(*ast.SelectorExpr); ok {
			if in, ok := i.X.(*ast.Ident); ok {
				return "*" + in.Name + "." + i.Sel.Name
			}

		}
	case *ast.SelectorExpr:
		if i, ok := v.X.(*ast.Ident); ok {
			return i.Name + "." + v.Sel.Name
		}
	case *ast.MapType:
		return "map[" + getFieldType(v.Key) + "]" + getFieldType(v.Value)
	case *ast.SliceExpr:
		return "[]" + getFieldType(v.X)
	}
	return ""
}

func getFieldTag(field *ast.Field) (rqlgenRes [3]string) {
	rawTag := strings.Trim(field.Tag.Value, "`")
	out := rexp.FindStringSubmatch(rawTag)
	rqlgenRes[0] = out[1]
	rqlgenRes[1] = out[2]
	out = rexpType.FindStringSubmatch(rawTag)
	if len(out) < 2 {
		fmt.Printf("Please, set correct base type for %v.%v\n", *pTypeName, field.Names[0].Name)
		os.Exit(1)
	}
	rqlgenRes[2] = out[1]
	return rqlgenRes
}

var rexp *regexp.Regexp
var rexpType *regexp.Regexp

type Field struct {
	Name          string
	DBName        string
	Type          string
	Omidempty     bool
	IsPolymorphic bool
	DBType        string
}

type Struct struct {
	Fields []*Field
}

var (
	pFileName  = flag.String("file", "", "file name")
	pTypeName  = flag.String("type", "", "type name")
	pPolyField = flag.String("poly", "", "polymorphic field")
)

func main() {
	flag.Parse()
	rexp = regexp.MustCompile(tagRex)
	rexpType = regexp.MustCompile(typeRex)
	var FileName = *pFileName
	var TypeName = *pTypeName
	var PolymorphicField = *pPolyField
	var genFileName = strings.TrimSuffix(FileName, ".go") + "_rqlgen.go"
	fset := token.NewFileSet()
	aFile, err := parser.ParseFile(fset, FileName, nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	strct := new(Struct)
	obj := aFile.Scope.Lookup(TypeName)
	if obj.Kind == ast.Typ {
		if t, ok := obj.Decl.(*ast.TypeSpec); ok {
			if s, ok := t.Type.(*ast.StructType); ok {
				for _, field := range s.Fields.List {
					f := new(Field)
					f.Name = field.Names[0].Name
					tags := getFieldTag(field)
					f.DBName = tags[0]
					f.Omidempty = len(tags[1]) != 0
					f.DBType = tags[2]
					f.Type = getFieldType(field.Type)
					f.IsPolymorphic = f.Name == PolymorphicField

					strct.Fields = append(strct.Fields, f)
				}

			}
		}
	}
	gen, err := os.Create(genFileName)
	defer func() {
		gen.Close()
		exec.Command("goimports", "-w", genFileName).Run()
		exec.Command("gofmt", "-s", "-w", genFileName).Run()
	}()
	if err != nil {
		panic(err)
	}
	shType := strings.ToLower(string(TypeName[0]))
	fmt.Fprintf(gen, "package %v\n", aFile.Name.Name)
	fmt.Fprintf(gen, "import (\n")
	for _, imp := range aFile.Imports {
		if imp.Name != nil {
			fmt.Fprintf(gen, "\t%v %v\n", imp.Name.Name, imp.Path.Value)
		} else {
			fmt.Fprintf(gen, "\t%v\n", imp.Path.Value)
		}

	}
	fmt.Fprintf(gen, "\tgenrqlerrors \"errors\"\n")
	fmt.Fprintf(gen, ")\n\n")
	fmt.Fprintf(gen, "func (%v *%v) MarshalRQL() (rqlgenRes interface{}, err error) {\n", shType, TypeName)
	fmt.Fprintf(gen, "\tif %v == nil {\n", shType)
	fmt.Fprintf(gen, "\t\treturn nil, nil\n")
	fmt.Fprintf(gen, "\t}\n")
	fmt.Fprintf(gen, "\trqlgenTmp := make(map[string]interface{}, %v)\n", len(strct.Fields))
	for _, field := range strct.Fields {
		switch field.DBType {
		case "string":
			fmt.Fprintf(gen, "\trqlgenTmp[\"%v\"] = string(%v.%v)\n", field.DBName, shType, field.Name)
		case "number":
			fmt.Fprintf(gen, "\trqlgenTmp[\"%v\"] = float64(%v.%v)\n", field.DBName, shType, field.Name)
		case "bool":
			fmt.Fprintf(gen, "\trqlgenTmp[\"%v\"] = bool(%v.%v)\n", field.DBName, shType, field.Name)
		case "time":
			fmt.Fprintf(gen, "\trqlgenTmp[\"%v\"] = time.Time(%v.%v)\n", field.DBName, shType, field.Name)
		case "rql":
			fmt.Fprintf(gen, "\trqlgenTmp[\"%v\"], err = %v.%v.MarshalRQL()\n", field.DBName, shType, field.Name)
			fmt.Fprintf(gen, "\tif err != nil {\n")
			fmt.Fprintf(gen, "\t\treturn nil, err\n")
			fmt.Fprintf(gen, "\t}\n")
		case "array_string":
			fmt.Fprintf(gen, "\t\t\tf%v := make([]string, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k] = string(v)\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "array_number":
			fmt.Fprintf(gen, "\t\t\tf%v := make([]float64, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k] = float64(v)\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "array_bool":
			fmt.Fprintf(gen, "\t\t\tf%v := make([]bool, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k] = bool(v)\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "array_time":
			fmt.Fprintf(gen, "\t\t\tf%v := make([]time.Time, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k] = time.Time(v)\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "array_rql":
			fmt.Fprintf(gen, "\t\t\tf%v := make([]interface{}, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k], err = v.MarshalRQL()\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif err != nil {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn nil, err\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "map_string":
			fmt.Fprintf(gen, "\t\t\tf%v := make(map[string]string, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k] = string(v)\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "map_number":
			fmt.Fprintf(gen, "\t\t\tf%v := make(map[string]float64, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k] = float64(v)\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "map_bool":
			fmt.Fprintf(gen, "\t\t\tf%v := make(map[string]bool, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k] = bool(v)\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "map_time":
			fmt.Fprintf(gen, "\t\t\tf%v := make(map[string]time.Time, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k] = time.Time(v)\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		case "map_rql":
			fmt.Fprintf(gen, "\t\t\tf%v := make(map[string]interface{}, len(%v.%[1]v))\n", field.Name, shType)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range %v.%v { \n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tf%v[k], err = v.MarshalRQL()\n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif err != nil {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn nil, err\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\trqlgenTmp[\"%v\"] = f%v\n", field.DBName, field.Name)
		default:
			fmt.Printf("Please, set correct base type for %v.%v\n", TypeName, field.Name)
			os.Exit(1)
		}
		//fmt.Fprintf(gen, "\trqlgenTmp[\"%v\"] = %v.%v\n", field.DBName, shType, field.Name)
	}
	fmt.Fprintf(gen, "\treturn rqlgenTmp, nil\n")
	fmt.Fprintf(gen, "}\n")

	fmt.Fprintf(gen, "\nfunc (%v *%v) UnmarshalRQL(rqlgenIface interface{}) ( err error) {\n", shType, TypeName)
	fmt.Fprintf(gen, "\tif %v == nil {\n", shType)
	fmt.Fprintf(gen, "\t\t%v = new(%v)\n", shType, TypeName)
	fmt.Fprintf(gen, "\t}\n")
	fmt.Fprintf(gen, "\tvar rqlgenTmp map[string]interface{}\n")
	fmt.Fprintf(gen, "\tvar ok bool\n")
	fmt.Fprintf(gen, "\tvar genrql interface{}\n")
	fmt.Fprintf(gen, "\tif rqlgenIface == nil {\n")
	fmt.Fprintf(gen, "\treturn nil\n")
	fmt.Fprintf(gen, "\t}\n")
	fmt.Fprintf(gen, "\trqlgenTmp, ok = rqlgenIface.(map[string]interface{})\n")
	fmt.Fprintf(gen, "\tif !ok {\n")
	fmt.Fprintf(gen, "\t\treturn genrqlerrors.New(\"Not converse interface{} to map[string]interface{} |\"+reflect.TypeOf(rqlgenIface).String())\n")
	fmt.Fprintf(gen, "\t}\n")
	for _, field := range strct.Fields {
		fmt.Fprintf(gen, "\tgenrql, ok = rqlgenTmp[\"%v\"]\n", field.DBName)
		fmt.Fprintf(gen, "\tif ok {\n")
		fmt.Fprintf(gen, "\t\tif genrql != nil {\n")
		switch field.DBType {
		case "string":
			fmt.Fprintf(gen, "\t\t\t%v.%v = (%v)(genrql.(string))\n", shType, field.Name, field.Type)
		case "number":
			fmt.Fprintf(gen, "\t\t\t%v.%v = (%v)(genrql.(float64))\n", shType, field.Name, field.Type)
		case "bool":
			fmt.Fprintf(gen, "\t\t\t%v.%v = (%v)(genrql.(bool))\n", shType, field.Name, field.Type)
		case "time":
			fmt.Fprintf(gen, "\t\t\t%v.%v = (%v)(genrql.(time.Time))\n", shType, field.Name, field.Type)
		case "rql":
			fmt.Fprintf(gen, "\t\t\terr = %v.%v.UnmarshalRQL(genrql)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\tif err != nil {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn  err\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "array_string":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.([]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn nil, genrqlerrors.New(\"Not converse interface{} to []interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\t%v.%v[k], ok = v.(string)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to string |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "array_number":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.([]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn  genrqlerrors.New(\"Not converse interface{} to []interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\t%v.%v[k], ok = v.(float64)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn  genrqlerrors.New(\"Not converse interface{} to float64 |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "array_bool":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.([]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to []interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\t%v.%v[k], ok = v.(bool)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to bool |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "array_time":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.([]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to []interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\t%v.%v[k], ok = v.(time.Time)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to time.Time |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "array_rql":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.([]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to []interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\terr = %v.%v[k].UmarshalRQL(v)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif err != nil {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn  err\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "map_string":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.(map[string]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to map[string]interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\t%v.%v[k], ok = v.(string)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to string |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "map_number":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.(map[string]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to map[string]interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\t%v.%v[k], ok = v.(float64)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to float64 |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "map_bool":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.(map[string]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn  genrqlerrors.New(\"Not converse interface{} to map[string]interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\t%v.%v[k], ok = v.(bool)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to bool |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "map_time":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.(map[string]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to map[string]interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\t%v.%v[k], ok = v.(time.Time)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to time.Time |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		case "map_rql":
			fmt.Fprintf(gen, "\t\t\tf%v,ok := genrql.(map[string]interface{})\n", field.Name)
			fmt.Fprintf(gen, "\t\t\tif !ok {\n")
			fmt.Fprintf(gen, "\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to map[string]interface{} |\"+reflect.TypeOf(genrql).String())\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t%v.%v = make(%v, len(f%[2]v))\n", shType, field.Name, field.Type)
			fmt.Fprintf(gen, "\t\t\tfor k, v:= range f%v { \n", field.Name)
			fmt.Fprintf(gen, "\t\t\t\terr = %v.%v[k].UmarshalRQL(v)\n", shType, field.Name)
			fmt.Fprintf(gen, "\t\t\t\tif err != nil {\n")
			fmt.Fprintf(gen, "\t\t\t\t\t\treturn err\n")
			fmt.Fprintf(gen, "\t\t\t\t}\n")
			fmt.Fprintf(gen, "\t\t\t}\n")
		default:
			fmt.Printf("Please, set correct base type for %v.%v\n", TypeName, field.Name)
			os.Exit(1)
		}
		fmt.Fprintf(gen, "\t\t}\n")
		if !field.Omidempty {
			fmt.Fprintf(gen, "\t} else {\n")
			fmt.Fprintf(gen, "\t\treturn genrqlerrors.New(\"Not found field: %v\")\n", field.DBName)
		}
		fmt.Fprintf(gen, "\t}\n")
		if field.IsPolymorphic {
			fmt.Fprintf(gen, "\t%v.mutate()\n", shType)
		}
	}
	fmt.Fprintf(gen, "\treturn nil\n")
	fmt.Fprintf(gen, "}\n")
}
