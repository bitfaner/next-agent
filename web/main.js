const feed = document.getElementById("stream-feed");
const copyButton = document.querySelector(".copy-button");

const events = [
  {
    time: "00:01",
    title: "workspace.create",
    body: "folder picker returns /home/zane/code/next-x and the path is stored locally."
  },
  {
    time: "00:03",
    title: "session.start",
    body: "backend launches codex with the workspace path as cwd and persists the session row."
  },
  {
    time: "00:06",
    title: "auth_request",
    body: "Codex requests approval for a filesystem operation; the card surfaces in the UI."
  },
  {
    time: "00:09",
    title: "message_delta",
    body: "streamed assistant output lands in the conversation without waiting for the full response."
  },
  {
    time: "00:12",
    title: "session.resume",
    body: "the stored thread id is loaded and the conversation can continue from history."
  }
];

function renderFeed(active = 0) {
  if (!feed) return;
  feed.innerHTML = "";
  const visible = events.slice(active, active + 3).concat(events.slice(0, Math.max(0, active + 3 - events.length)));
  const rows = [];

  visible.forEach((event, index) => {
    const row = document.createElement("div");
    row.className = "feed-row";
    row.style.setProperty("--row-index", index);
    row.innerHTML = `
      <div class="feed-time">${event.time}</div>
      <div class="feed-body">
        <strong>${event.title}</strong>
        <span>${event.body}</span>
      </div>
    `;
    if (index === 0) row.style.borderColor = "rgba(151, 212, 106, 0.35)";
    feed.appendChild(row);
    rows.push(row);
  });

  window.requestAnimationFrame(() => {
    window.requestAnimationFrame(() => {
      rows.forEach((row) => row.classList.add("is-entering"));
    });
  });
}

let pointer = 0;
renderFeed(pointer);

window.setInterval(() => {
  if (!feed || document.hidden) return;

  feed.classList.add("is-updating");
  window.setTimeout(() => {
    pointer = (pointer + 1) % events.length;
    renderFeed(pointer);
    feed.classList.remove("is-updating");
  }, 260);
}, 2400);

if (copyButton) {
  copyButton.addEventListener("click", async () => {
    const text = copyButton.getAttribute("data-copy") || "";
    try {
      await navigator.clipboard.writeText(text);
      copyButton.textContent = "copied";
      copyButton.dataset.state = "copied";
      window.setTimeout(() => {
        copyButton.textContent = "copy";
        delete copyButton.dataset.state;
      }, 1400);
    } catch {
      copyButton.textContent = "copy failed";
      window.setTimeout(() => {
        copyButton.textContent = "copy";
      }, 1400);
    }
  });
}
