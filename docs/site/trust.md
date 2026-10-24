---
title: Trust statement
section: security-review
order: 3
description: What you are trusting us with if we run awd for you. The full statement lives in the repository.
---

# Trust statement

If we run `awd` for you (the design-partner pilot), you are trusting us with some things. The statement is written for you to read before you sign, and it includes what is not yet as good as we want it.

**Read it here: [docs/hosting/trust.md](https://github.com/alchristleo/agentic-warden/blob/main/docs/hosting/trust.md).**

It is kept in one place on purpose, so this page cannot drift from it. It answers:

- what we run for you, and which parts other partners' cells share
- what we store, and what never reaches us (your code, prompts and model traffic)
- who holds the signing key, and what limits and exposes that
- where it runs, who can read your admin and SCIM tokens, and how backups and deletion work
- our subprocessors, and the pilot terms

Where the statement makes a claim about the infrastructure, the check is in the repository. The key policy, the task role's permissions, the private database and the digest-pinned image are asserted by `deploy/aws/cell/tests/cell.tftest.hcl`, and the [threat model](./threat-model.md) names the tests. Some claims are not in this repository: CloudTrail logging of `Sign` calls and the deletion steps are our operating practice, described in the [runbook](https://github.com/alchristleo/agentic-warden/blob/main/docs/hosting/runbook.md), and you should ask us for evidence of them.

One limit to know before you read it: we hold the AWS account that holds your signing key. Co-signed revisions, where a key you hold must also sign, are planned and not built.

If you run `awd` yourself, none of this applies. You hold the key, and the [security model](./security-model.md) is the page to read.
