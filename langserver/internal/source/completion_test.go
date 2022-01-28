package source_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kitagry/regols/langserver/internal/source"
)

type completionTestCase struct {
	files          map[string]source.File
	createLocation createLocationFunc
	expectItems    []source.CompletionItem
}

func TestProject_ListCompletionItemsStrict(t *testing.T) {
	tests := map[string]completionTestCase{
		"Should list import libarary": {
			files: map[string]source.File{
				"src.rego": {
					RawText: `package src

`,
				},
				"lib.rego": {
					RawText: `package lib`,
				},
			},
			createLocation: createLocation(3, 1, "src.rego"),
			expectItems: []source.CompletionItem{
				{
					Label:      "import data.lib",
					Kind:       source.ImportItem,
					InsertText: "import data.lib",
				},
			},
		},
		"Should not list already imported library": {
			files: map[string]source.File{
				"src.rego": {
					RawText: `package src

import data.lib
`,
				},
				"lib.rego": {
					RawText: `package lib`,
				},
			},
			createLocation: createLocation(4, 1, "src.rego"),
			expectItems:    []source.CompletionItem{},
		},
		"Should list variable in else clause": {
			files: map[string]source.File{
				"src.rego": {
					RawText: `package src

authorize = "allow" {
	msg := "allow"
	trace(msg)
} else = "deny" {
	ms := "deny"
	ms
}`,
				},
			},
			createLocation: createLocation(8, 3, "src.rego"),
			expectItems: []source.CompletionItem{
				{
					Label: "ms",
					Kind:  source.VariableItem,
				},
			},
		},
		"Should list rule as single item though the rule args are different": {
			files: map[string]source.File{
				"src.rego": {
					RawText: `package src

func() {
	me
}

mem_multiple("E") = 1000000000000000000000

mem_multiple("P") = 1000000000000000000`,
				},
			},
			createLocation: createLocation(4, 3, "src.rego"),
			expectItems: []source.CompletionItem{
				{
					Label:      "mem_multiple",
					Kind:       source.FunctionItem,
					InsertText: `mem_multiple("E")`,
					Detail: `mem_multiple("E") = 1000000000000000000000

mem_multiple("P") = 1000000000000000000`,
				},
			},
		},
		"Should not list duplicated variables": {
			files: map[string]source.File{
				"main.rego": {
					RawText: `package main

violation[msg] {
	msg = "hello"
	ms
}`,
				},
			},
			createLocation: createLocation(5, 3, "main.rego"),
			expectItems: []source.CompletionItem{
				{Label: "msg", Kind: source.VariableItem},
			},
		},
	}

	for n, tt := range tests {
		t.Run(n, func(t *testing.T) {
			project, err := source.NewProjectWithFiles(tt.files)
			if err != nil {
				t.Fatal(err)
			}

			location := tt.createLocation(tt.files)
			got, err := project.ListCompletionItems(location)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(tt.expectItems, got); diff != "" {
				t.Errorf("ListCompletionItems result diff (-expect, +got)\n%s", diff)
			}
			for _, e := range tt.expectItems {
				if !in(e, got) {
					t.Errorf("ListCompletionItems should return item %v, got %v", e, got)
				}
			}
		})
	}
}

func TestProject_ListCompletionItemsExist(t *testing.T) {
	tests := map[string]map[string]completionTestCase{
		"List variables": {
			"Should list variables in the same rule": {
				files: map[string]source.File{
					"main.rego": {
						RawText: `package main

violation[msg] {
	ms := hoge(fuga)
	messages[message]
	m
}`,
					},
				},
				createLocation: createLocation(6, 2, "main.rego"),
				expectItems: []source.CompletionItem{
					{Label: "msg", Kind: source.VariableItem},
					{Label: "ms", Kind: source.VariableItem},
					{Label: "message", Kind: source.VariableItem},
				},
			},
			"Should list imported variables": {
				files: map[string]source.File{
					"main.rego": {
						RawText: `package main

import data.lib

violation[msg] {
	l
}`,
					},
				},
				createLocation: createLocation(6, 2, "main.rego"),
				expectItems: []source.CompletionItem{
					{Label: "lib", Kind: source.PackageItem},
				},
			},
			"Should list variables when the prefix text is none": {
				files: map[string]source.File{
					"main.rego": {
						RawText: `package main

violation[msg] {
	msg = "hello"

}`,
					},
				},
				createLocation: createLocation(5, 1, "main.rego"),
				expectItems: []source.CompletionItem{
					{Label: "msg", Kind: source.VariableItem},
				},
			},
		},
		"List rules": {
			"Should list rules in the same file": {
				files: map[string]source.File{
					"main.rego": {
						RawText: `package main

violation [msg] {
	i
}

is_hello(msg) {
	msg == "hello"
}`,
					},
				},
				createLocation: createLocation(4, 2, "main.rego"),
				expectItems: []source.CompletionItem{
					{
						Label:      "is_hello",
						Kind:       source.FunctionItem,
						InsertText: "is_hello(msg)",
						Detail: `is_hello(msg) {
	msg == "hello"
}`,
					},
				},
			},
			"Should list rules in the same package but other file": {
				files: map[string]source.File{
					"main.rego": {
						RawText: `package main

violation [msg] {
	he
}`,
					},
					"other.rego": {
						RawText: `package main

hello(msg) {
	msg == "hello"
}`,
					},
				},
				createLocation: createLocation(4, 3, "main.rego"),
				expectItems: []source.CompletionItem{
					{
						Label:      "hello",
						Kind:       source.FunctionItem,
						InsertText: "hello(msg)",
						Detail: `hello(msg) {
	msg == "hello"
}`,
					},
				},
			},
			"Should list rules in the other packages": {
				files: map[string]source.File{
					"main.rego": {
						RawText: `package main

import data.lib

violation [msg] {
	lib.i
}`,
					},
					"lib.rego": {
						RawText: `package lib

is_hello(msg) {
	msg == "hello"
}`,
					},
				},
				createLocation: createLocation(6, 6, "main.rego"),
				expectItems: []source.CompletionItem{
					{
						Label:      "is_hello",
						Kind:       source.FunctionItem,
						InsertText: "is_hello(msg)",
						Detail: `is_hello(msg) {
	msg == "hello"
}`,
					},
				},
			},
			"Should list built-in functions": {
				files: map[string]source.File{
					"main.rego": {
						RawText: `package main

violation[msg] {
	j
}`,
					},
				},
				createLocation: createLocation(4, 2, "main.rego"),
				expectItems: []source.CompletionItem{
					{
						Label:      "json.patch",
						Kind:       source.BuiltinFunctionItem,
						Detail:     "json.patch(any, array[object<op: string, path: any>[any: any]])\n\n" + source.BuiltinDetail,
						InsertText: "json.patch(any, array[object<op: string, path: any>[any: any]])",
					},
				},
			},
			"Should list built-in functions when prefix include `.` character": {
				files: map[string]source.File{
					"main.rego": {
						RawText: `package main

violation[msg] {
	json.p
}`,
					},
				},
				createLocation: createLocation(4, 7, "main.rego"),
				expectItems: []source.CompletionItem{
					{
						Label:      "patch",
						Kind:       source.BuiltinFunctionItem,
						Detail:     "json.patch(any, array[object<op: string, path: any>[any: any]])\n\n" + source.BuiltinDetail,
						InsertText: "patch(any, array[object<op: string, path: any>[any: any]])",
					},
				},
			},
			"Should list rule which is variable": {
				files: map[string]source.File{
					"src.rego": {
						RawText: `package src

violation[msg] {
	is
}

default is_test = true`,
					},
				},
				createLocation: createLocation(4, 3, "src.rego"),
				expectItems: []source.CompletionItem{
					{
						Label:      "is_test",
						Kind:       source.VariableItem,
						InsertText: "is_test",
						Detail:     "default is_test = true",
					},
				},
			},
		},
		"List packages": {
			"Should list package items when the file is empty": {
				files: map[string]source.File{
					"test/core.rego": {
						RawText: ``,
					},
				},
				createLocation: createLocation(1, 1, "test/core.rego"),
				expectItems: []source.CompletionItem{
					{Label: "package core", Kind: source.PackageItem, InsertText: "package core"},
					{Label: "package test", Kind: source.PackageItem, InsertText: "package test"},
					{Label: "package test.core", Kind: source.PackageItem, InsertText: "package test.core"},
				},
			},
			"Should list package items when the file has no package": {
				files: map[string]source.File{
					"test/core.rego": {
						RawText: `p`,
					},
				},
				createLocation: createLocation(1, 1, "test/core.rego"),
				expectItems: []source.CompletionItem{
					{Label: "package core", Kind: source.PackageItem, InsertText: "package core"},
					{Label: "package test", Kind: source.PackageItem, InsertText: "package test"},
				},
			},
			`Should list package items which remove "_test"`: {
				files: map[string]source.File{
					"aaa/bbb_test.rego": {
						RawText: `p`,
					},
				},
				createLocation: createLocation(1, 1, "aaa/bbb_test.rego"),
				expectItems: []source.CompletionItem{
					{Label: "package aaa", Kind: source.PackageItem, InsertText: "package aaa"},
					{Label: "package bbb", Kind: source.PackageItem, InsertText: "package bbb"},
					{Label: "package aaa.bbb", Kind: source.PackageItem, InsertText: "package aaa.bbb"},
				},
			},
		},
	}

	for n, cases := range tests {
		t.Run(n, func(t *testing.T) {
			for n, tt := range cases {
				t.Run(n, func(t *testing.T) {
					project, err := source.NewProjectWithFiles(tt.files)
					if err != nil {
						t.Fatal(err)
					}

					location := tt.createLocation(tt.files)
					got, err := project.ListCompletionItems(location)
					if err != nil {
						t.Fatal(err)
					}

					for _, e := range tt.expectItems {
						if !in(e, got) {
							t.Errorf("ListCompletionItems should return item %v, got %v", e, got)
						}
					}
				})
			}
		})
	}
}

func in(item source.CompletionItem, list []source.CompletionItem) bool {
	for _, l := range list {
		if item == l {
			return true
		}
	}
	return false
}
