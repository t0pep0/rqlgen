package main

import (
	"fmt"
	"strings"
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
					code += fmt.Sprintf("\t\t\t\terr = %v.%v[k].UnmarshalRQL(v)\n", strct.ShortName, f.Name)
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
