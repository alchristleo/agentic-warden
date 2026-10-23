# Hosted cells: what you are trusting us with

This is the plain version for design partners, to read before you sign. It describes the pilot as it
runs today, including what is not yet as good as we want it.

## What we run for you

One isolated `awd` control plane at `https://<tenant>.<domain>`, in our AWS account. It has its own
database, secrets, signing key and IAM role. Other partners' cells share only the network, the load
balancer, the image repository and the container cluster.

## What we store

- Your policies, including their revision history.
- Machine records: machine id, the user email it enrolled as, the signing key id it last presented,
  and when it last synced.
- Identity-provider group membership, if you use SCIM or apply a group snapshot.
- Audit events: who applied what policy, and when.
- Application logs of the service, kept 30 days.

## What never reaches us

Your source code, your prompts, and model traffic. The agents run on your developers' machines and
talk to their model provider directly. `awd` serves policy down to machines and is never in the path
of an agent session. Machines send us enrollment and sync requests, and nothing else.

## Who holds the signing key

**We do.** Each cell has its own AWS KMS key (Ed25519) in our AWS account. The private half never
leaves KMS, and nobody, including us, can read it out. But we control the account, so a person with
the right access in it could cause a signature to be made. What limits and exposes that:

- Only your cell's service role may sign with your key, and only that key.
- Every `Sign` call is recorded in AWS CloudTrail. We will share the log of your key's signatures
  on request.
- Machines pin your key when they enroll and check every policy bundle against it.

This does not protect you from us. A signature made by the cell is valid whether the policy came
from you or from someone with access to our account. **Co-signed revisions**, where a key you hold
must also sign, are planned and not built yet.

Rotating the key today means re-enrolling your machines. We will tell you before we do it and agree
a date with you.

## Where it runs

In one AWS region, named in your order. Database, backups, logs and key stay in that region. Today
we run one region; if you need another, we will tell you whether we can before you sign.

## Access to your data

The cell's admin token and SCIM token are generated for the cell and given to you out of band. Our
operators can read them, because they are in our infrastructure state and in Secrets Manager. We use
them only to support you, when you ask. We do not log in to your database as a routine; only the
service itself has access to it.

## Backups and deletion

The database keeps automated backups for 7 days. When you leave, or ask us to delete the cell:

- We delete the database. Its automated backups go with it, and no final snapshot is kept.
- The signing key enters AWS's 7-day deletion window and cannot be recovered after it.
- Secrets, logs and the cell's infrastructure are deleted. The infrastructure state file and its
  versions, which hold the old secrets, are deleted too.
- We do this within 30 days of your written request, and tell you when it is done.

## Subprocessors

Amazon Web Services is our only subprocessor. We use ECS Fargate, RDS, KMS, Secrets Manager,
CloudWatch, S3, Route 53, ACM, ECR and CloudTrail.

## Pilot terms

This is a design-partner pilot: no billing, no availability promise, one database instance per cell,
no alarms or paging yet, and no compliance certification. If any of that matters to you, ask before
you sign.
