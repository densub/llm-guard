package llamacpp

import (
	"fmt"
	"strings"
)

// jsonStringArrayGrammar is a GBNF grammar (llama.cpp's grammar-constrained
// decoding format) that forces the model's output to be a JSON array of
// strings, e.g. `[]` or `["Project Bluefin", "Jane Smith"]`. This lets us
// parse the response with encoding/json without worrying about the model
// adding prose, markdown fences, or malformed JSON.
const jsonStringArrayGrammar = `root   ::= "[" ws ( string ( "," ws string )* )? ws "]"
string ::= "\"" char* "\"" ws
char   ::= [^"\\\x00-\x1f] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})
ws     ::= [ \t\n\r]*
`

// jsonStringArrayOfArraysGrammar forces output like `[[]]` or `[["a"],["b","c"]]`.
const jsonStringArrayOfArraysGrammar = `root   ::= "[" ws ( array ( "," ws array )* )? ws "]"
array  ::= "[" ws ( string ( "," ws string )* )? ws "]"
string ::= "\"" char* "\"" ws
char   ::= [^"\\\x00-\x1f] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})
ws     ::= [ \t\n\r]*
`

// systemPrompt and userPromptTemplate are formatted into Qwen's ChatML
// template (<|im_start|>role ... <|im_end|>) in buildPrompt. Sending a raw
// instruction to /completion without this template produces much worse
// results for this model, since "Instruct" models are tuned to respond
// within their chat format.
const systemPrompt = `You are a privacy filter that finds sensitive substrings in text and returns them as a JSON array of strings.`

const userPromptTemplate = `Identify any sensitive information in the TEXT below that should NOT be shared with a third-party AI service: full names of people, company/organization names, internal project codenames, customer/account identifiers, physical addresses, phone numbers, or other confidential details. Do not flag generic or public information.

Respond with ONLY a JSON array of the exact substrings to redact, copied verbatim from TEXT. If nothing is sensitive, respond with [].

TEXT:
%s`

const batchUserPromptTemplate = `Identify sensitive information in each numbered TEXT block below. Respond with ONLY a JSON array of JSON string arrays: one inner array per TEXT block, in the same order. Each inner array lists exact substrings to redact from that block, copied verbatim. Use [] for blocks with nothing sensitive.

%s`

// buildPrompt returns the ChatML-formatted prompt sent to llama-server's
// /completion endpoint for the given text.
func buildPrompt(text string) string {
	return "<|im_start|>system\n" + systemPrompt + "<|im_end|>\n" +
		"<|im_start|>user\n" + fmt.Sprintf(userPromptTemplate, text) + "<|im_end|>\n" +
		"<|im_start|>assistant\n"
}

func buildBatchPrompt(texts []string) string {
	var blocks strings.Builder
	for i, text := range texts {
		fmt.Fprintf(&blocks, "TEXT %d:\n%s\n\n", i, text)
	}
	return "<|im_start|>system\n" + systemPrompt + "<|im_end|>\n" +
		"<|im_start|>user\n" + fmt.Sprintf(batchUserPromptTemplate, blocks.String()) + "<|im_end|>\n" +
		"<|im_start|>assistant\n"
}
