# proto

The wire contract between the [Akili](https://github.com/goakili/akili) control plane (`../server`) and
the agent (`../agent`): versioned envelopes and frames, the tool catalog with
risk levels, the policy engine, and redaction.

Both sides import it; it imports neither. Every envelope and frame has a golden file in
`testdata/golden`, and the prompt-injection corpus in `testdata/injection` runs against every built-in
policy at every autonomy level.

```bash
go test ./...
go vet ./...
```

## License

Apache License 2.0. See [LICENSE](LICENSE).
