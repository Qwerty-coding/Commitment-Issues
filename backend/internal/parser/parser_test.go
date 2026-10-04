package parser

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	js "github.com/smacker/go-tree-sitter/javascript"
	py "github.com/smacker/go-tree-sitter/python"
)

func parseWith(t *testing.T, lang *sitter.Language, code string) *sitter.Tree {
	t.Helper()
	p := sitter.NewParser()
	p.SetLanguage(lang)
	tree, err := p.ParseCtx(context.Background(), nil, []byte(code))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if tree == nil {
		t.Fatal("parse returned nil tree")
	}
	return tree
}

func findFunction(elements []CodeElement, name string) (CodeElement, bool) {
	for _, el := range elements {
		if el.Name == name {
			return el, true
		}
	}
	return CodeElement{}, false
}

func TestExtractDataForFile_SetsFileOnEveryElement(t *testing.T) {
	code := "function login() { return 1; }\nvar token = 'x';\n"
	tree := parseWith(t, js.GetLanguage(), code)

	ctx := ASTContext{}
	ExtractDataForFile(tree.RootNode(), []byte(code), "auth.js", &ctx)

	for _, fn := range ctx.Functions {
		if fn.File != "auth.js" {
			t.Errorf("function %s file = %q, want auth.js", fn.Name, fn.File)
		}
	}
	for _, v := range ctx.Variables {
		if v.File != "auth.js" {
			t.Errorf("variable %s file = %q, want auth.js", v.Name, v.File)
		}
	}
}

func TestExtractData_PythonScopeAndSignature(t *testing.T) {
	code := `def outer(a, b):
    def inner(c):
        return c
    return inner(a)


class Engine:
    def charge(self, token):
        return token
`
	tree := parseWith(t, py.GetLanguage(), code)
	ctx := ASTContext{}
	ExtractData(tree.RootNode(), []byte(code), &ctx)

	outer, ok := findFunction(ctx.Functions, "outer")
	if !ok {
		t.Fatalf("outer not extracted: %#v", ctx.Functions)
	}
	if outer.Scope != "" {
		t.Errorf("outer scope = %q, want empty (module level)", outer.Scope)
	}

	inner, ok := findFunction(ctx.Functions, "inner")
	if !ok {
		t.Fatalf("inner not extracted: %#v", ctx.Functions)
	}
	if inner.Scope != "outer" {
		t.Errorf("nested function scope = %q, want outer", inner.Scope)
	}

	engine, ok := findFunction(ctx.Functions, "Engine")
	if !ok {
		t.Fatalf("Engine class not extracted")
	}
	if engine.Scope != "" {
		t.Errorf("Engine scope = %q, want empty", engine.Scope)
	}

	charge, ok := findFunction(ctx.Functions, "charge")
	if !ok {
		t.Fatalf("charge method not extracted: %#v", ctx.Functions)
	}
	if charge.Scope != "Engine" {
		t.Errorf("method scope = %q, want Engine", charge.Scope)
	}
	if charge.Signature == "" {
		t.Errorf("method signature should be captured")
	}
}

func TestExtractData_JavaScriptSignatureForFunctionAndArrow(t *testing.T) {
	code := "function add(a, b) { return a + b; }\nconst mul = (x, y) => x * y;\n"
	tree := parseWith(t, js.GetLanguage(), code)
	ctx := ASTContext{}
	ExtractData(tree.RootNode(), []byte(code), &ctx)

	add, ok := findFunction(ctx.Functions, "add")
	if !ok {
		t.Fatalf("add not extracted: %#v", ctx.Functions)
	}
	if add.Signature != "(a, b)" {
		t.Errorf("add signature = %q, want (a, b)", add.Signature)
	}

	mul, ok := findFunction(ctx.Functions, "mul")
	if !ok {
		t.Fatalf("mul not extracted as function: %#v", ctx.Functions)
	}
	if mul.Signature == "" {
		t.Errorf("arrow function signature should be captured")
	}
}

func TestParseAndExtract_Tier1_Go(t *testing.T) {
	code := `package main

import "fmt"

type Server struct {
	port int
}

type Handler interface {
	Serve()
}

var maxConns = 100

func Start() {
	fmt.Println("starting")
}

func (s *Server) Handle(req string) string {
	res := "ok"
	return res
}
`
	ctx, err := ParseAndExtract("server.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "go" {
		t.Errorf("language = %q, want go", ctx.Language)
	}
	if len(ctx.Imports) == 0 {
		t.Errorf("expected imports to be extracted")
	}

	startFn, ok := findFunction(ctx.Functions, "Start")
	if !ok {
		t.Fatalf("Start function not found in functions: %#v", ctx.Functions)
	}
	if startFn.Kind != "Function" {
		t.Errorf("Start kind = %q, want Function", startFn.Kind)
	}
	if len(startFn.Calls) == 0 || startFn.Calls[0] != "fmt.Println" {
		t.Errorf("expected fmt.Println call recorded, got %v", startFn.Calls)
	}

	handleMethod, ok := findFunction(ctx.Functions, "Handle")
	if !ok {
		t.Fatalf("Handle method not found")
	}
	if handleMethod.Kind != "Method" {
		t.Errorf("Handle kind = %q, want Method", handleMethod.Kind)
	}
	if handleMethod.Scope != "*Server" {
		t.Errorf("Handle scope = %q, want *Server", handleMethod.Scope)
	}
	if handleMethod.Signature != "(req string)" {
		t.Errorf("Handle signature = %q, want (req string)", handleMethod.Signature)
	}

	serverStruct, ok := findFunction(ctx.Functions, "Server")
	if !ok || serverStruct.Kind != "Struct" {
		t.Errorf("Server struct not found with Kind: Struct")
	}
	handlerInterface, ok := findFunction(ctx.Functions, "Handler")
	if !ok || handlerInterface.Kind != "Interface" {
		t.Errorf("Handler interface not found with Kind: Interface")
	}
}

func TestParseAndExtract_Tier1_TypeScript(t *testing.T) {
	code := `import { config } from './config';

export interface User {
	id: string;
}

export class UserService {
	constructor(private db: any) {}

	public findUser(id: string): User {
		return this.db.get(id);
	}
}

export const fetchAll = async () => [];
`
	ctx, err := ParseAndExtract("user.service.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "typescript" {
		t.Errorf("language = %q, want typescript", ctx.Language)
	}
	if len(ctx.Imports) == 0 {
		t.Errorf("expected imports to be extracted")
	}

	cls, ok := findFunction(ctx.Functions, "UserService")
	if !ok || cls.Kind != "Class" {
		t.Errorf("UserService class not found")
	}

	ctor, ok := findFunction(ctx.Functions, "constructor")
	if !ok || ctor.Kind != "Constructor" {
		t.Errorf("constructor not found with Kind: Constructor")
	}
	if ctor.Scope != "UserService" {
		t.Errorf("constructor scope = %q, want UserService", ctor.Scope)
	}

	method, ok := findFunction(ctx.Functions, "findUser")
	if !ok || method.Kind != "Method" {
		t.Errorf("findUser method not found")
	}
	if method.Scope != "UserService" {
		t.Errorf("findUser scope = %q, want UserService", method.Scope)
	}

	arrow, ok := findFunction(ctx.Functions, "fetchAll")
	if !ok || arrow.Kind != "Function" {
		t.Errorf("fetchAll arrow function not found")
	}
}

func TestParseAndExtract_Tier2_Java(t *testing.T) {
	code := `package com.example;

import java.util.List;

public class PaymentService {
	private String apiKey;

	public PaymentService(String key) {
		this.apiKey = key;
	}

	public void process(int id) {
		execute(id);
	}

	public void process(String name) {
		execute(name);
	}
}
`
	ctx, err := ParseAndExtract("PaymentService.java", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "java" {
		t.Errorf("language = %q, want java", ctx.Language)
	}
	if len(ctx.Imports) == 0 {
		t.Errorf("expected import to be extracted")
	}

	cls, ok := findFunction(ctx.Functions, "PaymentService")
	if !ok || cls.Kind != "Class" {
		t.Fatalf("PaymentService class not found")
	}

	// Verify overloads: both process methods extracted with distinct signatures
	var processes []CodeElement
	for _, fn := range ctx.Functions {
		if fn.Name == "process" {
			processes = append(processes, fn)
		}
	}
	if len(processes) != 2 {
		t.Fatalf("expected 2 overloaded process methods, got %d", len(processes))
	}
	if processes[0].Signature == processes[1].Signature {
		t.Errorf("overloads should have distinct signatures: %s vs %s", processes[0].Signature, processes[1].Signature)
	}
	for _, p := range processes {
		if p.Scope != "PaymentService" {
			t.Errorf("method scope = %q, want PaymentService", p.Scope)
		}
	}
}

func TestParseAndExtract_Tier2_CSharp(t *testing.T) {
	code := `using System;

namespace Enterprise.App {
	public class OrderProcessor {
		public OrderProcessor() {}

		public void Process() {
			Save();
		}
	}

	public struct OrderSummary {
		public int Total;
	}
}
`
	ctx, err := ParseAndExtract("Processor.cs", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "csharp" {
		t.Errorf("language = %q, want csharp", ctx.Language)
	}
	if len(ctx.Imports) == 0 {
		t.Errorf("expected using directive to be extracted")
	}

	cls, ok := findFunction(ctx.Functions, "OrderProcessor")
	if !ok || cls.Kind != "Class" {
		t.Fatalf("OrderProcessor class not found")
	}
	if cls.Scope != "Enterprise.App" {
		t.Errorf("class scope = %q, want Enterprise.App", cls.Scope)
	}

	method, ok := findFunction(ctx.Functions, "Process")
	if !ok || method.Kind != "Method" {
		t.Fatalf("Process method not found")
	}
	if method.Scope != "Enterprise.App.OrderProcessor" {
		t.Errorf("method scope = %q, want Enterprise.App.OrderProcessor", method.Scope)
	}

	st, ok := findFunction(ctx.Functions, "OrderSummary")
	if !ok || st.Kind != "Struct" {
		t.Errorf("OrderSummary struct not found")
	}
}

func TestParseAndExtract_Tier2_CPP(t *testing.T) {
	code := `#include <iostream>

class Engine {
public:
	void start() {
		std::cout << "vroom";
	}
};

int main() {
	return 0;
}
`
	ctx, err := ParseAndExtract("main.cpp", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "cpp" {
		t.Errorf("language = %q, want cpp", ctx.Language)
	}
	if len(ctx.Imports) == 0 {
		t.Errorf("expected #include to be extracted")
	}

	eng, ok := findFunction(ctx.Functions, "Engine")
	if !ok || eng.Kind != "Class" {
		t.Errorf("Engine class not found")
	}

	mainFn, ok := findFunction(ctx.Functions, "main")
	if !ok || mainFn.Kind != "Function" {
		t.Errorf("main function not found")
	}
}

func TestParseAndExtract_Tier3_Rust(t *testing.T) {
	code := `use std::collections::HashMap;

struct Client {
	id: u64,
}

impl Client {
	fn connect(&self) {
		println!("connecting");
	}
}

fn run() {
	let x = 1;
}
`
	ctx, err := ParseAndExtract("lib.rs", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "rust" {
		t.Errorf("language = %q, want rust", ctx.Language)
	}
	if len(ctx.Imports) == 0 {
		t.Errorf("expected use statement to be extracted")
	}

	st, ok := findFunction(ctx.Functions, "Client")
	if !ok || st.Kind != "Struct" {
		t.Errorf("Client struct not found")
	}

	conn, ok := findFunction(ctx.Functions, "connect")
	if !ok || conn.Kind != "Method" {
		t.Fatalf("connect method not found")
	}
	if conn.Scope != "Client" {
		t.Errorf("connect method scope = %q, want Client", conn.Scope)
	}

	runFn, ok := findFunction(ctx.Functions, "run")
	if !ok || runFn.Kind != "Function" {
		t.Errorf("run function not found")
	}
}

func TestParseAndExtract_Tier3_Ruby(t *testing.T) {
	code := `require 'json'

module Finance
	class Ledger
		def post(entry)
			@entries << entry
		end

		def self.audit
			true
		end
	end
end
`
	ctx, err := ParseAndExtract("ledger.rb", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "ruby" {
		t.Errorf("language = %q, want ruby", ctx.Language)
	}

	mod, ok := findFunction(ctx.Functions, "Finance")
	if !ok || mod.Kind != "Module" {
		t.Errorf("Finance module not found")
	}

	cls, ok := findFunction(ctx.Functions, "Ledger")
	if !ok || cls.Kind != "Class" {
		t.Errorf("Ledger class not found")
	}
	if cls.Scope != "Finance" {
		t.Errorf("Ledger scope = %q, want Finance", cls.Scope)
	}

	postMethod, ok := findFunction(ctx.Functions, "post")
	if !ok || postMethod.Kind != "Method" {
		t.Errorf("post method not found")
	}
	if postMethod.Scope != "Finance.Ledger" {
		t.Errorf("post scope = %q, want Finance.Ledger", postMethod.Scope)
	}

	auditMethod, ok := findFunction(ctx.Functions, "audit")
	if !ok || auditMethod.Kind != "Method" {
		t.Errorf("audit singleton method not found")
	}
}

func TestParseAndExtract_Tier3_PHP(t *testing.T) {
	code := `<?php
namespace App\Services;

use App\Models\User;

class AuthManager {
	public function authenticate($token) {
		return true;
	}
}
`
	ctx, err := ParseAndExtract("auth.php", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "php" {
		t.Errorf("language = %q, want php", ctx.Language)
	}
	if len(ctx.Imports) == 0 {
		t.Errorf("expected namespace use to be extracted")
	}

	cls, ok := findFunction(ctx.Functions, "AuthManager")
	if !ok || cls.Kind != "Class" {
		t.Errorf("AuthManager class not found")
	}

	authMethod, ok := findFunction(ctx.Functions, "authenticate")
	if !ok || authMethod.Kind != "Method" {
		t.Errorf("authenticate method not found")
	}
}

func TestParseAndExtract_Tier3_HTML(t *testing.T) {
	code := `<!DOCTYPE html>
<html>
<head><title>Test</title></head>
<body>
	<div id="app">
		<button>Submit</button>
	</div>
</body>
</html>
`
	ctx, err := ParseAndExtract("index.html", []byte(code))
	if err != nil {
		t.Fatalf("ParseAndExtract failed: %v", err)
	}
	if ctx.Language != "html" {
		t.Errorf("language = %q, want html", ctx.Language)
	}
	if len(ctx.Functions) == 0 {
		t.Errorf("expected HTML elements to be extracted")
	}
}

func TestParseAndExtract_Safeguards(t *testing.T) {
	// 1. Binary file with null bytes
	binaryData := []byte("PK\x03\x04\x00\x00\x00\x00some zip content")
	_, err := ParseAndExtract("archive.zip", binaryData)
	if err != ErrBinaryFile {
		t.Errorf("expected ErrBinaryFile, got %v", err)
	}

	// 2. Unsupported extension
	unsupportedData := []byte("SELECT * FROM users;")
	ctx, err := ParseAndExtract("query.xyz", unsupportedData)
	if err != nil {
		t.Errorf("unsupported file should not return fatal error, got %v", err)
	}
	if ctx.Language != "unsupported" {
		t.Errorf("language = %q, want unsupported", ctx.Language)
	}
	if len(ctx.Diagnostics) == 0 || ctx.Diagnostics[0].Severity != "warning" {
		t.Errorf("expected warning diagnostic for unsupported language, got %#v", ctx.Diagnostics)
	}

	// 3. Oversized file
	oversized := make([]byte, 11*1024*1024)
	for i := range oversized {
		oversized[i] = 'a'
	}
	_, err = ParseAndExtract("huge.py", oversized)
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestParseAndExtract_DiagnosticsOnSyntaxError(t *testing.T) {
	brokenPython := "def broken(:\n    pass\n"
	ctx, err := ParseAndExtract("broken.py", []byte(brokenPython))
	if err != nil {
		t.Fatalf("ParseAndExtract should complete and collect diagnostics, got err: %v", err)
	}
	if len(ctx.Diagnostics) == 0 {
		t.Errorf("expected diagnostics to record syntax error")
	}
	hasError := false
	for _, d := range ctx.Diagnostics {
		if d.Severity == "error" {
			hasError = true
			if d.Line == 0 {
				t.Errorf("diagnostic line should be > 0, got %d", d.Line)
			}
		}
	}
	if !hasError {
		t.Errorf("expected error severity diagnostic in %#v", ctx.Diagnostics)
	}

	// ParseAndCheckSyntax should return non-nil error
	if err := ParseAndCheckSyntax("broken.py", []byte(brokenPython)); err == nil {
		t.Errorf("expected ParseAndCheckSyntax to return error for broken python")
	}
}

