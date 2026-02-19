# Schema Reference

grubber is schema-agnostic: any valid YAML in frontmatter or code blocks will be extracted. The schema below is an example for personal knowledge management.

## Frontmatter

```yaml
---
title:
keywords: []
created:
---
```

Frontmatter fields are merged into every record from that file. On field name collision, the YAML block wins.

## Record Types

### person

```yaml
type: person
name: "Alice Johnson"
org: "ACME Corp"
role: vendor
last_contact: 2025-01-20
region: US-West
projects: []
status: active
```

### contract

```yaml
type: contract
name: "Cloud Hosting Agreement"
partner: "ACME Corp"
owner: Jane Smith
number: SLA-2024-001
amount: 12000
currency: USD
status: active
start: 2024-06-01
end: 2025-05-31
```

### item

```yaml
type: item
name: "Dell PowerEdge R740"
vendor: "Dell Technologies"
status: active
category: [Hardware, Server]
purchased: 2024-03-15
amount: 4200
currency: USD
owner: IT Department
count: 3
```

### event

```yaml
type: event
name: "Weekly Sync Q1"
org: "Northwind Corp"
start: 2025-02-03T10:00
end: 2025-02-03T10:30
participants: [Jane Smith, Bob Lee]
owner: Jane Smith
```

### project

```yaml
type: project
name: "Project Alpha"
org: "Northwind Corp"
start: 2025-01-15
end: 2025-06-30
status: active
review: 2025-03-01
owner: Jane Smith
```

### ticket

```yaml
type: ticket
name: "Migrate DNS records"
status: todo
start: 2025-02-15
end:
owner: Bob Lee
```

These are examples. Add any fields you need — grubber extracts whatever YAML it finds.

## Design Principles

- `type` and `name` on every record — universal identification
- snake_case consistently — no camelCase exceptions
- `start`/`end` for time ranges — `end` means expiration, never "last updated"
- Specific field names for different semantics — `org` (affiliation), `partner` (contractual), `vendor` (seller)
- `amount` + `currency` separated — enables calculations and comparisons
- Date fields are tolerant — ISO datetime preferred (`2025-01-15T14:00`), date-only allowed (`2025-01-15`)
- Plural for arrays — `participants`, `projects`
- English field names throughout — no localized terms in field names
- YAML block wins over frontmatter — on field name collision, the block value takes precedence
- Frontmatter for note metadata only — `type` belongs in the YAML block, not frontmatter
