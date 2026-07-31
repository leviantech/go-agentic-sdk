#!/bin/bash
# Terima argumen JSON via stdin: {"name": "Budi"}
input=$(cat)
name=$(echo "$input" | sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [ -z "$name" ]; then
  name="dunia"
fi
echo "Halo, $name! Ini balasan dari script skill."
