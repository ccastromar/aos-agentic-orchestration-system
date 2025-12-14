package llm

const intentSchemaJSON = `
{
  "type": "object",
  "required": ["intent", "parameters"],
  "additionalProperties": false,
  "properties": {
    "intent": {
      "type": "string",
      "minLength": 1
    },
	"language":{
		"type": "string"
	},    
	"confidence": {
      "type": "number",
      "minimum": 0,
      "maximum": 1
    },
    "parameters": {
      "type": "object",
      "additionalProperties": {
        "type": "string"
      }
    },
    "errors": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}
`
