// Exports the youtube.com cookies of the window this popup was opened in as a
// Netscape cookies.txt, the format yt-dlp and a YTMDL media session read.
// The file is built in memory and handed to the browser's own download; the
// extension makes no network request and stores nothing.

const button = document.getElementById("export");
const status = document.getElementById("status");

function say(text, kind) {
  status.textContent = text;
  status.dataset.kind = kind || "";
}

// The cookie store of the window the popup belongs to. A private window has
// its own store; without this the export would silently read the normal one.
async function storeOfThisWindow() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  const stores = await chrome.cookies.getAllCookieStores();
  const match = tab ? stores.find((s) => s.tabIds.includes(tab.id)) : undefined;
  return match ? match.id : undefined;
}

// One line per cookie:
// domain, include subdomains, path, secure, expiry (unix seconds), name, value.
function toNetscape(cookies) {
  const lines = ["# Netscape HTTP Cookie File", ""];
  for (const c of cookies) {
    const domain = c.hostOnly ? c.domain : c.domain.startsWith(".") ? c.domain : "." + c.domain;
    lines.push(
      [
        (c.httpOnly ? "#HttpOnly_" : "") + domain,
        c.hostOnly ? "FALSE" : "TRUE",
        c.path,
        c.secure ? "TRUE" : "FALSE",
        c.session || !c.expirationDate ? "0" : String(Math.floor(c.expirationDate)),
        c.name,
        c.value,
      ].join("\t"),
    );
  }
  return lines.join("\n") + "\n";
}

function save(text) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = "cookies.txt";
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

button.addEventListener("click", async () => {
  button.disabled = true;
  say("");
  try {
    const storeId = await storeOfThisWindow();
    const cookies = await chrome.cookies.getAll({ domain: "youtube.com", ...(storeId ? { storeId } : {}) });
    if (cookies.length === 0) {
      say("Keine Cookies von youtube.com in diesem Fenster. Erst dort anmelden.", "error");
      return;
    }
    // Without a login cookie the export is an anonymous session and would
    // meet the same bot check on the server.
    const signedIn = cookies.some((c) => c.name === "SID" || c.name === "__Secure-3PSID");
    save(toNetscape(cookies));
    say(
      signedIn
        ? `${cookies.length} Cookies gespeichert.`
        : `${cookies.length} Cookies gespeichert, aber ohne Anmeldung. Erst bei YouTube anmelden.`,
      signedIn ? "ok" : "error",
    );
  } catch (err) {
    say("Export fehlgeschlagen: " + (err && err.message ? err.message : String(err)), "error");
  } finally {
    button.disabled = false;
  }
});
