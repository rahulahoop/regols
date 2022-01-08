package cache

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/ast/location"
)

func (p *Project) LookupDefinition(location *location.Location) ([]*ast.Location, error) {
	targetTerm, err := p.searchTargetTerm(location)
	if err != nil {
		return nil, err
	}
	if targetTerm == nil {
		return nil, nil
	}

	return p.findDefinition(targetTerm, location.File), nil
}

func (p *Project) searchTargetTerm(location *location.Location) (*ast.Term, error) {
	module := p.GetModule(location.File)
	if module == nil {
		return nil, nil
	}

	for _, r := range module.Rules {
		if !in(location, r.Loc()) {
			continue
		}
		term, err := p.searchTargetTermInRule(location, r)
		return term, err
	}
	return nil, nil
}

func (p *Project) searchRuleForTerm(term *ast.Term) *ast.Rule {
	module := p.GetModule(term.Loc().File)
	if module == nil {
		return nil
	}

	for _, r := range module.Rules {
		if in(term.Loc(), r.Loc()) {
			return r
		}
	}
	return nil
}

func (p *Project) searchTargetTermInRule(location *location.Location, rule *ast.Rule) (*ast.Term, error) {
	for _, b := range rule.Body {
		if !in(location, b.Loc()) {
			continue
		}

		switch t := b.Terms.(type) {
		case *ast.Term:
			return p.searchTargetTermInTerm(location, t)
		case []*ast.Term:
			return p.searchTargetTermInTerms(location, t)
		}
	}
	return nil, nil
}

func (p *Project) searchTargetTermInTerms(location *location.Location, terms []*ast.Term) (*ast.Term, error) {
	for _, t := range terms {
		if in(location, t.Loc()) {
			return p.searchTargetTermInTerm(location, t)
		}
	}
	return nil, nil
}

func (p *Project) searchTargetTermInTerm(loc *location.Location, term *ast.Term) (*ast.Term, error) {
	switch v := term.Value.(type) {
	case ast.Call:
		return p.searchTargetTermInTerms(loc, []*ast.Term(v))
	case ast.Ref:
		if len(v) == 1 {
			return p.searchTargetTermInTerm(loc, v[0])
		}
		if len(v) >= 2 {
			// This is for imported method
			// If you use the following code.
			// ```
			// import data.lib.util
			// util.test[hoge]
			// ```
			// Then
			// util.test[hoge] <- ast.Ref
			// util <- ast.Var
			// test <- ast.String
			// I think this is a bit wrong...
			// https://www.openpolicyagent.org/docs/latest/policy-reference/#grammar
			_, ok1 := v[0].Value.(ast.Var)
			_, ok2 := v[1].Value.(ast.String)
			if ok1 && ok2 && (in(loc, v[0].Loc()) || in(loc, v[1].Loc())) {
				value := ast.Ref{v[0], v[1]}
				loc := v[0].Loc()
				return &ast.Term{Value: value, Location: &location.Location{
					Text:   []byte(value.String()),
					File:   loc.File,
					Row:    loc.Row,
					Col:    loc.Col,
					Offset: loc.Offset,
				}}, nil
			}
		}
		return p.searchTargetTermInTerms(loc, []*ast.Term(v))
	case *ast.Array:
		for i := 0; i < v.Len(); i++ {
			t, err := p.searchTargetTermInTerm(loc, v.Elem(i))
			if err != nil {
				return nil, err
			}
			if t == nil {
				continue
			}
			if in(loc, t.Loc()) {
				return t, nil
			}
		}
		return nil, nil
	case ast.Var:
		return term, nil
	case ast.String, ast.Boolean, ast.Number:
		return nil, nil
	default:
		return nil, fmt.Errorf("not supported type %T: %v\n", v, v)
	}
}

func (p *Project) findDefinition(term *ast.Term, path string) []*ast.Location {
	rule := p.searchRuleForTerm(term)
	if rule != nil {
		target := p.findDefinitionInRule(term, rule)
		if target != nil {
			return []*ast.Location{target.Loc()}
		}
	}
	return p.findDefinitionInModule(term, path)
}

func (p *Project) findDefinitionInRule(term *ast.Term, rule *ast.Rule) *ast.Term {
	if t, ok := term.Value.(ast.Ref); ok && len(t) > 1 {
		term = t[0]
	}

	// violation[msg]
	//           ^ this is key
	if rule.Head.Key != nil {
		result := p.findDefinitionInTerm(term, rule.Head.Key)
		if result != nil {
			return result
		}
	}

	// func(hello)
	//      ^ this is arg
	result := p.findDefinitionInTerms(term, rule.Head.Args)
	if result != nil {
		return result
	}

	for _, b := range rule.Body {
		switch t := b.Terms.(type) {
		case *ast.Term:
			result := p.findDefinitionInTerm(term, t)
			if result != nil {
				return result
			}
		case []*ast.Term:
			// equality -> [hoge, fuga] = split_hoge()
			// assign -> hoge := fuga()
			if ast.Equality.Ref().Equal(b.Operator()) || ast.Assign.Ref().Equal(b.Operator()) {
				result := p.findDefinitionInTerm(term, t[1])
				if result != nil {
					return result
				}
			}
		default:
			fmt.Fprintf(os.Stderr, "type: %T", b.Terms)
		}
	}
	return nil
}

func (p *Project) findDefinitionInTerms(target *ast.Term, terms []*ast.Term) *ast.Term {
	for _, term := range terms {
		t := p.findDefinitionInTerm(target, term)
		if t != nil {
			return t
		}
	}
	return nil
}

func (p *Project) findDefinitionInTerm(target *ast.Term, term *ast.Term) *ast.Term {
	switch v := term.Value.(type) {
	case ast.Call:
		return p.findDefinitionInTerms(target, []*ast.Term(v))
	case ast.Ref:
		// import data.a
		// a.b[c] -> a: ast.Var, b: ast.String, c: ast.Var
		// a.b.c  -> a: ast.Var, b: ast.String, c: ast.String
		return p.findDefinitionInTerms(target, []*ast.Term(v)[1:])
	case *ast.Array:
		for i := 0; i < v.Len(); i++ {
			t := p.findDefinitionInTerm(target, v.Elem(i))
			if t == nil {
				continue
			}
			return t
		}
		return nil
	case ast.Var:
		if target.Equal(term) && target.Loc().Offset > term.Loc().Offset {
			return term
		}
		return nil
	case ast.String, ast.Boolean, ast.Number:
		return nil
	default:
		return nil
	}
}

func (p *Project) findDefinitionInModule(term *ast.Term, path string) []*ast.Location {
	searchPackageName := p.findPolicyRef(term)
	searchPolicies := p.findPolicies(searchPackageName)

	if len(searchPolicies) == 0 {
		return nil
	}

	word := term.String()
	if strings.Contains(word, ".") /* imported method */ {
		word = word[strings.Index(word, ".")+1:]
	}

	result := make([]*ast.Location, 0)
	for _, mod := range searchPolicies {
		for _, rule := range mod.Rules {
			if rule.Head.Name.String() == word {
				r := rule
				result = append(result, r.Loc())
			}
		}
	}
	return result
}

func (p *Project) findPolicyRef(term *ast.Term) ast.Ref {
	module := p.GetModule(term.Loc().File)
	if module == nil {
		return nil
	}

	if ref, ok := term.Value.(ast.Ref); ok && len(ref) > 1 {
		imp := findImportOutsidePolicy(ref[0].String(), module.Imports)
		if imp == nil {
			return nil
		}
		result, ok := imp.Path.Value.(ast.Ref)
		if !ok {
			return nil
		}
		return result
	}

	return module.Package.Path
}

func findImportOutsidePolicy(moduleName string, imports []*ast.Import) *ast.Import {
	for _, imp := range imports {
		alias := imp.Alias.String()
		if alias != "" && moduleName == alias {
			imp := imp
			return imp
		}
		m := imp.String()[strings.LastIndex(imp.String(), "."):]
		if strings.HasSuffix(m, moduleName) {
			imp := imp
			return imp
		}
	}
	return nil
}

func (p *Project) findPolicies(packageName ast.Ref) []*ast.Module {
	modules, err := p.getModules()
	if err != nil {
		return nil
	}
	result := make([]*ast.Module, 0)
	for _, module := range modules {
		if module.Package.Path.Equal(packageName) {
			result = append(result, module)
		}
	}
	return result
}

func in(target, src *location.Location) bool {
	return target.Offset >= src.Offset && target.Offset <= (src.Offset+len(src.Text))
}

func (p *Project) GetRawText(path string) (string, error) {
	if f, ok := p.GetFile(path); ok {
		return f.RowText, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	buf.ReadFrom(f)
	return buf.String(), nil
}
