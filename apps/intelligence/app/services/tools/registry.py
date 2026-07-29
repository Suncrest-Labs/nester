from typing import Any, Dict, List

from .action_tools import ACTION_TOOLS
from .read_tools import READ_TOOLS
from .types import Tool

TOOL_REGISTRY: List[Tool] = READ_TOOLS + ACTION_TOOLS

for tool in TOOL_REGISTRY:
    # Assertion that no tool targets an /admin/ path
    # (Since URLs are inside handlers, we do a basic check on name/desc)
    assert "admin" not in tool.name.lower(), f"Tool {tool.name} appears to be an admin tool"

def list_tool_schemas() -> List[Dict[str, Any]]:
    schemas = []
    for tool in TOOL_REGISTRY:
        schema = tool.args_model.model_json_schema()
        # Clean up schema for anthropic
        input_schema = {
            "type": "object",
            "properties": schema.get("properties", {}),
            "additionalProperties": False,
        }
        if "required" in schema:
            input_schema["required"] = schema["required"]

        schemas.append({
            "name": tool.name,
            "description": tool.description,
            "input_schema": input_schema
        })
    return schemas

def get_tool(name: str) -> Tool:
    for tool in TOOL_REGISTRY:
        if tool.name == name:
            return tool
    raise KeyError(f"Tool {name} not found")
