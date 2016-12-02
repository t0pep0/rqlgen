package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const (
	tagRex  = `gorethink:"([a-zA-Z0-9\-\_]*)[,]?(omidempty)?[a-zA-Z0-9\-\_\,]*"`
	typeRex = `rqlgen:"(number|string|time|map_string|map_number|map_time|map_bool|map_rql|array_string|array_number|array_time|array_bool|array_rql|bool|rql)"`
)

type Field struct {
	Name          string
	DBName        string
	Type          string
	Omidempty     bool
	IsPolymorphic bool
	DBType        string
}

func (f *Field) getMarshalStr(strct *Struct) (code string) {
	gType, ok := dbType2Runtime[f.DBType]
	if ok {
		if gType == f.Type {
			return fmt.Sprintf("\trqlgenTmp[\"%v\"] = %v.%v\n", f.DBName, strct.ShortName, f.Name)
		} else {
			return fmt.Sprintf("\trqlgenTmp[\"%v\"] = %v(%v.%v)\n", f.DBName, gType, strct.ShortName, f.Name)
		}
	}
	if f.DBType == "rql" {
		code = fmt.Sprintf("\trqlgenTmp[\"%v\"], err = %v.%v.MarshalRQL()\n", f.DBName, strct.ShortName, f.Name)
		code += fmt.Sprintf("\tif err != nil {\n")
		code += fmt.Sprintf("\t\treturn nil, err\n")
		code += fmt.Sprintf("\t}\n")
		return code
	}
	dbType := strings.Split(f.DBType, "_")
	if gType, ok = dbType2Runtime[dbType[1]]; ok {
		if dbType[0] == "array" {
			code = fmt.Sprintf("\t\t\tf%v := make([]%v, len(%v.%[1]v))\n", f.Name, gType, strct.ShortName)
		} else {
			code = fmt.Sprintf("\t\t\tf%v := make(map[string]%v, len(%v.%[1]v))\n", f.Name, gType, strct.ShortName)
		}
		code += fmt.Sprintf("\t\t\tfor k, v:= range %v.%v { \n", strct.ShortName, f.Name)
		if gType == dbType[1] {
			code += fmt.Sprintf("\t\t\t\tf%v[k] = v\n", f.Name)
		} else {
			code += fmt.Sprintf("\t\t\t\tf%v[k], ok = %v(v)\n", f.Name, f.Type)
			code += fmt.Sprintf("\t\t\t\tif !ok {\n")
			code += fmt.Sprintf("\t\t\t\t\treturn nil, genrqlerrors.New(\"Can't convert %v to %v (%v.%v)\")\n", f.Type, f.DBType, strct.Name, f.Name)
			code += fmt.Sprintf("\t\t\t\t}\n")
		}
		code += fmt.Sprintf("\t\t\t}\n")
		code += fmt.Sprintf("\t\t\trqlgenTmp[\"%v\"] = f%v\n", f.DBName, f.Name)
		return code
	}
	if dbType[1] == "rql" {
		if dbType[0] == "array" {
			code = fmt.Sprintf("\t\t\tf%v := make([]interface{}, len(%v.%[1]v))\n", f.Name, strct.ShortName)
		} else {
			code = fmt.Sprintf("\t\t\tf%v := make(map[string]interface{}, len(%v.%[1]v))\n", f.Name, strct.ShortName)
		}
		code += fmt.Sprintf("\t\t\tfor k, v:= range %v.%v { \n", strct.ShortName, f.Name)
		code += fmt.Sprintf("\t\t\t\tf%v[k], err = v.MarshalRQL()\n", f.Name)
		code += fmt.Sprintf("\t\t\t\tif err != nil {\n")
		code += fmt.Sprintf("\t\t\t\treturn nil, err\n")
		code += fmt.Sprintf("\t\t\t\t}\n")
		code += fmt.Sprintf("\t\t\t}\n")
		code += fmt.Sprintf("\t\t\trqlgenTmp[\"%v\"] = f%v\n", f.DBName, f.Name)
		return code
	}
	return code
}

func (f *Field) getUnmarshalStr(strct *Struct) (code string) {
	code = fmt.Sprintf("\tgenrql, ok = rqlgenTmp[\"%v\"]\n", f.DBName)
	code += fmt.Sprintf("\tif ok {\n")
	code += fmt.Sprintf("\t\tif genrql != nil {\n")
	gType, ok := dbType2Runtime[f.DBType]
	if ok {
		if gType == f.Type {
			code += fmt.Sprintf("\t\t\t%v.%v, ok = genrql.(%v)\n", strct.ShortName, f.Name, f.Type)
			code += fmt.Sprintf("\t\t\t\tif !ok {\n")
			code += fmt.Sprintf("\t\t\t\treturn genrqlerrors.New(\"Can't convert \"+reflect.TypeOf(genrql).String()+\" to %v (%v.%v)\")\n", f.Type, strct.Name, f.Name)
			code += fmt.Sprintf("\t\t\t\t}\n")
		} else {
			code += fmt.Sprintf("\t\t\ttmp%v%v, ok := genrql.(%v)\n", strct.ShortName, f.Name, gType)
			code += fmt.Sprintf("\t\t\t\tif !ok {\n")
			code += fmt.Sprintf("\t\t\t\treturn genrqlerrors.New(\"Can't convert \"+reflect.TypeOf(genrql).String()+\" to %v (%v.%v)\")\n", gType, strct.Name, f.Name)
			code += fmt.Sprintf("\t\t\t\t}\n")
			code += fmt.Sprintf("\t\t\t%v.%v = %v(tmp%[1]v%[2]v)\n", strct.ShortName, f.Name, f.Type)
		}
	} else {
		if f.DBType == "rql" {
			code += fmt.Sprintf("\t\t\terr = %v.%v.UnmarshalRQL(genrql)\n", strct.ShortName, f.Name)
			code += fmt.Sprintf("\t\t\tif err != nil {\n")
			code += fmt.Sprintf("\t\t\t\treturn  err\n")
			code += fmt.Sprintf("\t\t\t}\n")
		} else {
			dbType := strings.Split(f.DBType, "_")
			if dbType[0] == "array" {
				code += fmt.Sprintf("\t\t\tf%v,ok := genrql.([]interface{})\n", f.Name)
				code += fmt.Sprintf("\t\t\tif !ok {\n")
				code += fmt.Sprintf("\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to []interface{} |\"+reflect.TypeOf(genrql).String())\n")
				code += fmt.Sprintf("\t\t\t}\n")
			} else {
				code += fmt.Sprintf("\t\t\tf%v,ok := genrql.(map[string]interface{})\n", f.Name)
				code += fmt.Sprintf("\t\t\tif !ok {\n")
				code += fmt.Sprintf("\t\t\t\treturn genrqlerrors.New(\"Not converse interface{} to map[string]interface{} |\"+reflect.TypeOf(genrql).String())\n")
				code += fmt.Sprintf("\t\t\t}\n")
			}
			code += fmt.Sprintf("\t\t\t%v.%v = make(%v, len(f%[2]v))\n", strct.ShortName, f.Name, f.Type)
			if gType, ok = dbType2Runtime[dbType[1]]; ok {
				code += fmt.Sprintf("\t\t\tfor k, v:= range f%v { \n", f.Name)
				if gType == dbType[1] {
					code += fmt.Sprintf("\t\t\t\t%v.%v[k], ok = v.(%v)\n", strct.ShortName, f.Name, gType)
					code += fmt.Sprintf("\t\t\t\tif !ok {\n")
					code += fmt.Sprintf("\t\t\t\t\treturn genrqlerrors.New(\"Can't convert \"+reflect.TypeOf(genrql).String()+\" to %v (%v.%v)\")\n", gType, strct.Name, f.Name)
					code += fmt.Sprintf("\t\t\t\t}\n")
				} else {
					code += fmt.Sprintf("\t\t\t\ttmp%v%v, ok = v.(%v)\n", strct.ShortName, f.Name, gType)
					code += fmt.Sprintf("\t\t\t\tif !ok {\n")
					code += fmt.Sprintf("\t\t\t\t\treturn genrqlerrors.New(\"Can't convert \"+reflect.TypeOf(genrql).String()+\" to %v (%v.%v)\")\n", gType, strct.Name, f.Name)
					code += fmt.Sprintf("\t\t\t\t}\n")
					code += fmt.Sprintf("\t\t\t\t%v.%v[k] = %v(tmp%[1]v%[2]v)\n", strct.ShortName, f.Name, f.Type)
				}
				code += fmt.Sprintf("\t\t\t}\n")
			} else {
				if dbType[1] == "rql" {
					code += fmt.Sprintf("\t\t\tfor k, v:= range f%v { \n", f.Name)
					code += fmt.Sprintf("\t\t\t\terr = %v.%v[k].UmarshalRQL(v)\n", strct.Name, f.Name)
					code += fmt.Sprintf("\t\t\t\tif err != nil {\n")
					code += fmt.Sprintf("\t\t\t\t\t\treturn  err\n")
					code += fmt.Sprintf("\t\t\t\t}\n")
					code += fmt.Sprintf("\t\t\t}\n")
				}
			}
		}
	}
	code += fmt.Sprintf("\t\t}\n")
	if !f.Omidempty {
		code += fmt.Sprintf("\t} else {\n")
		code += fmt.Sprintf("\t\treturn genrqlerrors.New(\"Not found field: %v\")\n", f.DBName)
	}
	code += fmt.Sprintf("\t}\n")
	if f.IsPolymorphic {
		code += fmt.Sprintf("\t%v.mutate()\n", strct.ShortName)
	}
	return code
}

type Struct struct {
	Fields    []*Field
	Name      string
	ShortName string
}

func (strct *Struct) writeMarshal(gen io.Writer) {
	fmt.Fprintf(gen, "func (%v *%v) MarshalRQL() (rqlgenRes interface{}, err error) {\n", strct.ShortName, strct.Name)
	fmt.Fprintf(gen, "\tif %v == nil {\n", strct.ShortName)
	fmt.Fprintf(gen, "\t\treturn nil, nil\n")
	fmt.Fprintf(gen, "\t}\n")
	fmt.Fprintf(gen, "\trqlgenTmp := make(map[string]interface{}, %v)\n", len(strct.Fields))
	for _, field := range strct.Fields {
		fmt.Fprintf(gen, field.getMarshalStr(strct))
	}
	fmt.Fprintf(gen, "\treturn rqlgenTmp, nil\n")
	fmt.Fprintf(gen, "}\n")
}

func (strct *Struct) writeUnmarshal(gen io.Writer) {
	fmt.Fprintf(gen, "\nfunc (%v *%v) UnmarshalRQL(rqlgenIface interface{}) ( err error) {\n", strct.ShortName, strct.Name)
	fmt.Fprintf(gen, "\tif %v == nil {\n", strct.ShortName)
	fmt.Fprintf(gen, "\t\t%v = new(%v)\n", strct.ShortName, strct.Name)
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
		fmt.Fprintf(gen, field.getUnmarshalStr(strct))
	}
	fmt.Fprintf(gen, "\treturn nil\n")
	fmt.Fprintf(gen, "}\n")
}

var (
	pFileName      = flag.String("file", "", "file name")
	pTypeName      = flag.String("type", "", "type name")
	pPolyField     = flag.String("poly", "", "polymorphic field")
	rexp           *regexp.Regexp
	rexpType       *regexp.Regexp
	dbType2Runtime = map[string]string{
		"string": "string",
		"number": "float64",
		"bool":   "bool",
		"time":   "time.Time",
	}
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

func getFieldTag(field *ast.Field) (rqlgenRes [3]string, err error) {
	rawTag := strings.Trim(field.Tag.Value, "`")
	out := rexp.FindStringSubmatch(rawTag)
	rqlgenRes[0] = out[1]
	rqlgenRes[1] = out[2]
	out = rexpType.FindStringSubmatch(rawTag)
	if len(out) < 2 {
		err = fmt.Errorf("Please, set correct base type for %v.%v\n", *pTypeName, field.Names[0].Name)
		return rqlgenRes, err
	}
	rqlgenRes[2] = out[1]
	return rqlgenRes, nil
}

func readStruct(obj *ast.Object, polyField string) (strct *Struct, err error) {
	if obj == nil {
		return nil, errors.New("Not found target struct")
	}
	strct = new(Struct)
	strct.Name = obj.Name
	strct.ShortName = strings.ToLower(string(strct.Name[0]))
	if obj.Kind == ast.Typ {
		if t, ok := obj.Decl.(*ast.TypeSpec); ok {
			if s, ok := t.Type.(*ast.StructType); ok {
				for _, field := range s.Fields.List {
					f := new(Field)
					f.Name = field.Names[0].Name
					tags, err := getFieldTag(field)
					if err != nil {
						return nil, err
					}
					f.DBName = tags[0]
					f.Omidempty = len(tags[1]) != 0
					f.DBType = tags[2]
					f.Type = getFieldType(field.Type)
					f.IsPolymorphic = f.Name == polyField

					strct.Fields = append(strct.Fields, f)
				}

			} else {
				return nil, errors.New("Tagret isn't struct")
			}
		} else {
			return nil, errors.New("Tagret isn't type")
		}
	} else {
		return nil, errors.New("Tagret isn't type")
	}
	return strct, nil
}

func writeHeader(gen io.Writer, src *ast.File) {
	fmt.Fprintf(gen, "//This file is generated by rqlgen. DO NOT EDIT!\n")
	fmt.Fprintf(gen, "package %v\n", src.Name.Name)
	fmt.Fprintf(gen, "import (\n")
	for _, imp := range src.Imports {
		if imp.Name != nil {
			fmt.Fprintf(gen, "\t%v %v\n", imp.Name.Name, imp.Path.Value)
		} else {
			fmt.Fprintf(gen, "\t%v\n", imp.Path.Value)
		}

	}
	fmt.Fprintf(gen, "\tgenrqlerrors \"errors\"\n")
	fmt.Fprintf(gen, ")\n\n")
}

func main() {
	flag.Parse()
	var genFileName = strings.TrimSuffix(*pFileName, ".go") + "_rqlgen.go"

	rexp = regexp.MustCompile(tagRex)
	rexpType = regexp.MustCompile(typeRex)

	fset := token.NewFileSet()
	aFile, err := parser.ParseFile(fset, *pFileName, nil, parser.ParseComments)
	if err != nil {
		fmt.Println("Unable to open file", err)
		os.Exit(1)
	}
	obj := aFile.Scope.Lookup(*pTypeName)
	strct, err := readStruct(obj, *pPolyField)
	if err != nil {
		fmt.Println("Unable to parse file", err)
		os.Exit(2)
	}
	gen, err := os.Create(genFileName)
	if err != nil {
		fmt.Println("Unable to create file", err)
		os.Exit(3)
	}
	defer func() {
		gen.Close()
		exec.Command("goimports", "-w", genFileName).Run()
		exec.Command("gofmt", "-s", "-w", genFileName).Run()
	}()

	writeHeader(gen, aFile)
	strct.writeMarshal(gen)
	strct.writeUnmarshal(gen)

}
