export function SignIn() {
  return (
    <main className="mx-auto max-w-sm p-8 text-center">
      <h1 className="text-2xl font-semibold">Agentic Warden console</h1>
      <p className="mt-2 text-muted-foreground">Sign in with your company account.</p>
      <a className="mt-6 inline-block rounded-md bg-primary px-4 py-2 text-primary-foreground" href="/console/auth/login">
        Sign in
      </a>
    </main>
  );
}
