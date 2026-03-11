"use client"

export default function GlobalError({ reset }: { error: Error; reset: () => void }) {
  return (
    <html>
      <body>
        <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }}>
          <div style={{ textAlign: "center" }}>
            <h1 style={{ fontSize: "1.5rem", fontWeight: 300, marginBottom: "1rem" }}>Something went wrong</h1>
            <button
              onClick={reset}
              style={{ padding: "0.5rem 1.5rem", border: "1px solid #ccc", borderRadius: "9999px", cursor: "pointer", background: "transparent" }}
            >
              Try again
            </button>
          </div>
        </div>
      </body>
    </html>
  );
}
