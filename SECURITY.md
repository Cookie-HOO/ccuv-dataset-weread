# Security Policy

Do not report or commit API keys, cookies, account identifiers, reading records, raw gateway bodies, or release signing material.

`WEREAD_API_KEY` is process-inherited secret material. The dataset must not persist it, demonstrate it in examples, place it in a command line, or include it in stdout/stderr diagnostics. Tests must use synthetic data only.

For a security report, open a private GitHub security advisory for this repository. Do not publish credential-bearing reproduction data in a public issue.
