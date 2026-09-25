import { useMutation } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { api, ApiError } from "@/api";
import type { EnrollmentToken } from "@/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export function Enrollment() {
  const [user, setUser] = useState("");
  const [ttl, setTtl] = useState("24h");
  const [result, setResult] = useState<EnrollmentToken | null>(null);
  const [copied, setCopied] = useState(false);

  const mutation = useMutation({
    mutationFn: () => api.post<EnrollmentToken>("/v1/enrollment-tokens", { user, ttl }),
    onSuccess: (token) => {
      setResult(token);
      setCopied(false);
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    setResult(null);
    mutation.mutate();
  }

  async function copy() {
    if (!result) return;
    try {
      await navigator.clipboard.writeText(result.token);
      setCopied(true);
    } catch {
      // Clipboard access can be denied by the browser; nothing more to do.
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Enrollment</h1>
      <form onSubmit={submit} className="flex max-w-sm flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="enroll-user">User</Label>
          <Input
            id="enroll-user"
            type="email"
            required
            value={user}
            onChange={(e) => setUser(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="enroll-ttl">Valid for</Label>
          <Input
            id="enroll-ttl"
            value={ttl}
            onChange={(e) => setTtl(e.target.value)}
            aria-describedby="enroll-ttl-hint"
          />
          <p id="enroll-ttl-hint" className="text-xs text-muted-foreground">
            At most 168h.
          </p>
        </div>
        {mutation.isError && (
          <p role="alert" className="text-sm text-destructive">
            {mutation.error instanceof ApiError ? mutation.error.message : "Could not create the token."}
          </p>
        )}
        <Button type="submit" disabled={mutation.isPending}>
          Create token
        </Button>
      </form>
      {result && (
        <Card className="max-w-sm">
          <CardHeader>
            <CardTitle>Token for {result.user} — shown once</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <code className="rounded bg-muted px-2 py-1">{result.token}</code>
              <Button type="button" variant="outline" size="sm" onClick={copy}>
                {copied ? "Copied" : "Copy"}
              </Button>
            </div>
            <pre className="overflow-x-auto rounded bg-muted p-2 text-xs">
              {`aw-sync enroll --server ${location.origin} --token ${result.token}`}
            </pre>
            <p className="text-sm text-muted-foreground">
              Copy it now: it cannot be retrieved again.
            </p>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
