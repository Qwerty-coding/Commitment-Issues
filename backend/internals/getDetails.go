package internals

func GetDetails() map[string]string {
	return map[string]string{
		"file":       "main.go",
		"current":    "mahak",
		"incoming":   "main",
		"suggestion": "Use the incoming changes because they contain the latest implementation.",
		"repo" : "check",
		"confidence": "94%",
	}

}
