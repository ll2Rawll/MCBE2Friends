"use strict";
const byId = (id) => document.getElementById(id);
const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
function base32(bytes) {
  let text = "",
    bits = 0,
    carry = 0;
  for (const byte of bytes) {
    carry = (carry << 8) | byte;
    bits += 8;
    while (bits >= 5) {
      bits -= 5;
      text += alphabet[(carry >>> bits) & 31];
    }
  }
  if (bits) text += alphabet[(carry << (5 - bits)) & 31];
  return text;
}
byId("pc").addEventListener("input", () => {
  byId("grant").hidden = true;
  byId("result").textContent = "";
});
byId("key-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const pc = byId("pc").value.trim().toUpperCase();
  const result = byId("result");
  byId("grant").hidden = true;
  if (!/^PC-[A-Z2-7]{16}$/.test(pc)) {
    result.textContent = "Enter the complete PC code from the launcher.";
    return;
  }
  try {
    const marker = byId("marker").value;
    const raw = base32(crypto.getRandomValues(new Uint8Array(16)));
    const data = new TextEncoder().encode(marker + "\n" + pc + "\nM2F" + raw);
    const hash = Array.from(
      new Uint8Array(await crypto.subtle.digest("SHA-256", data)),
      (n) => n.toString(16).padStart(2, "0"),
    ).join("");
    if (byId("pc").value.trim().toUpperCase() !== pc) return;
    const path = "grants/" + hash + ".txt";
    byId("buyer-key").value = "M2F-" + raw.match(/.{1,5}/g).join("-");
    byId("grant-path").value = path;
    const url = new URL(
      "https://github.com/ll2Rawll/MCBE2Friends/new/premium-keys",
    );
    url.searchParams.set("filename", path);
    url.searchParams.set("value", marker + "\n");
    byId("approve").href = url.toString();
    byId("grant").hidden = false;
    result.textContent =
      "Key generated locally. Commit the approval before sending it.";
  } catch {
    result.textContent =
      "Open this page over HTTPS in a browser with Web Crypto.";
  }
});
byId("copy-key").addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(byId("buyer-key").value);
    byId("result").textContent = "Buyer key copied.";
  } catch {
    byId("buyer-key").select();
    byId("result").textContent = "The key is selected. Use Copy to copy it.";
  }
});
