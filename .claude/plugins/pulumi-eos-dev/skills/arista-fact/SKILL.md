---
description: Verify an Arista EOS feature, command, or behaviour against the official documentation indexed in arista-mcp. Pass the topic as the argument (e.g. `EVPN multihoming ESI Type-1`, `port-channel lacp fallback`). Always use this before claiming an Arista fact.
argument-hint: <topic>
allowed-tools: mcp__arista-mcp__search_docs mcp__arista-mcp__get_document mcp__arista-mcp__lookup_section
---

# Verify an Arista fact via arista-mcp

Topic: `$ARGUMENTS`

Procedure:

1. Call `mcp__arista-mcp__search_docs` with the topic as `query` and `topK: 8`.
2. Inspect the top hits' `section_title`, `version`, and `score`. Prefer manual entries (`category: "manual"`) over TOIs unless the topic is feature-specific.
3. If the section excerpt is incomplete, call `mcp__arista-mcp__lookup_section` with the `documentId` and exact `section_title`.
4. Cite findings as `EOS User Manual §X.Y.Z` (for manual hits) or `TOI <number>` (for TOI hits) with the doc id and the EOS version.
5. Never paraphrase a CLI syntax or default value without a citation.

This skill is the project's mandatory ground-truth check before adding or modifying any Arista-specific resource shape, validation rule, or default value.
