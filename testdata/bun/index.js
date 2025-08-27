import { appendFile } from "fs/promises";

const server = Bun.serve({
  port: 3000,
  async fetch(request) {
    // Create timestamped log entry
    const timestamp = new Date().toISOString();
    const logEntry = `${timestamp} - ${request.method} ${request.url} - ${request.headers.get("user-agent") || "Unknown"}\n`;

    try {
      // Append to log file in the specified directory
      await appendFile("/miren/data/local/server.log", logEntry);
    } catch (error) {
      console.error("Failed to write to log file:", error);
    }

    return new Response("Welcome to Bun!");
  },
});

console.log(`Listening on ${server.url}`);
console.log("Logging requests to /miren/data/local/server.log");
