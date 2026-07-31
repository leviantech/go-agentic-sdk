---
name: hello-skill
description: Demo skill that greets users and reports the local time.
version: 1.0.0
tools:
  - name: greet
    description: Greets someone by name.
    command: scripts/greet.sh
    parameters:
      type: object
      properties:
        name:
          type: string
          description: The name of the person being greeted.
  - name: get_local_time
    description: Returns the current local time of the server.
    command: scripts/time.sh
    parameters:
      type: object
      properties: {}
---

# Hello Skill

You are a friendly assistant. When the user greets you, use the `greet` tool
to greet back. When the user asks for the time, use the `get_local_time` tool.

Use the tool first, then compose the answer from its result.
