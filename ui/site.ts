export {};
const $ = <T extends Element = HTMLElement>(
  s: string,
  root: ParentNode = document,
) => root.querySelector<T>(s);
const csrf = $<HTMLMetaElement>("meta[name=csrf-token]")?.content || "";
async function api(path: string, method = "GET", body?: unknown) {
  const headers: Record<string, string> = { "X-CSRF-Token": csrf };
  if (body !== undefined && !(body instanceof FormData))
    headers["Content-Type"] = "application/json";
  const r = await fetch(path, {
    method,
    headers,
    body:
      body instanceof FormData
        ? body
        : body === undefined
          ? undefined
          : JSON.stringify(body),
  });
  const data = r.status === 204 ? {} : await r.json();
  if (!r.ok) throw new Error(data.error || "Request failed. Please try again.");
  return data;
}
let toastTimer: ReturnType<typeof setTimeout>;
function toast(message: string) {
  const el = $("#toast")!;
  el.textContent = message;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (el.hidden = true), 6000);
}
function status(form: Element, message: string, error = false) {
  const el = $(".form-status", form);
  if (el) {
    el.textContent = message;
    el.classList.toggle("error", error);
    if (error) {
      el.setAttribute("tabindex", "-1");
      (el as HTMLElement).focus();
    }
  } else toast(message);
}
const fields = (f: HTMLFormElement) =>
  Object.fromEntries(new FormData(f).entries()) as Record<string, string>;
async function busy(button: HTMLButtonElement, fn: () => Promise<void>) {
  button.disabled = true;
  try {
    await fn();
  } catch (e) {
    toast((e as Error).message);
  } finally {
    button.disabled = false;
  }
}
function ask(
  title: string,
  initial = "",
  label = "Details",
): Promise<string | null> {
  const dialog = $<HTMLDialogElement>("#input-dialog")!,
    input = $<HTMLTextAreaElement>("#dialog-input")!;
  $("#dialog-title")!.textContent = title;
  $("#dialog-label")!.textContent = label;
  input.value = initial;
  dialog.showModal();
  input.focus();
  return new Promise((resolve) =>
    dialog.addEventListener(
      "close",
      () => resolve(dialog.returnValue === "confirm" ? input.value : null),
      { once: true },
    ),
  );
}
for (const f of document.querySelectorAll<HTMLFormElement>(
  "form[data-auth],form[data-settings],form[data-admin-form]",
))
  f.addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const b = $<HTMLButtonElement>("button", f)!;
    b.disabled = true;
    status(f, "Saving…");
    try {
      const kind = f.dataset.auth
          ? "auth"
          : f.dataset.settings
            ? "account"
            : "admin",
        action = f.dataset.auth || f.dataset.settings || f.dataset.adminForm;
      const data = await api(`/api/${kind}/${action}`, "POST", fields(f));
      status(f, data.message || "Saved.");
      if (data.redirect) location.href = data.redirect;
      else if (action === "password") location.href = "/login";
      else if (kind === "admin") location.reload();
    } catch (e) {
      status(f, (e as Error).message, true);
    } finally {
      b.disabled = false;
    }
  });
for (const b of document.querySelectorAll<HTMLButtonElement>("[data-account]"))
  b.addEventListener("click", () =>
    busy(b, async () => {
      await api("/api/account/" + b.dataset.account, "POST", {});
      if (b.dataset.account === "logout" || b.dataset.account === "revoke")
        location.href = "/";
      else if (b.dataset.account === "notifications") location.reload();
      else toast("Check your inbox for the verification email.");
    }),
  );
for (const b of document.querySelectorAll<HTMLButtonElement>(
  "[data-interaction]",
))
  b.addEventListener("click", () =>
    busy(b, async () => {
      const on = b.getAttribute("aria-pressed") === "true";
      await api(
        `/api/stories/${encodeURIComponent(b.dataset.slug!)}/${b.dataset.interaction}`,
        on ? "DELETE" : "POST",
        {},
      );
      location.reload();
    }),
  );
function wireFollow(b: HTMLButtonElement) {
  b.addEventListener("click", () =>
    busy(b, async () => {
      const on = b.getAttribute("aria-pressed") === "true";
      await api(
        `/api/follows/${b.dataset.follow}/${encodeURIComponent(b.dataset.target!)}`,
        on ? "DELETE" : "POST",
        {},
      );
      b.setAttribute("aria-pressed", String(!on));
      b.textContent = on ? "Follow" : "Following";
    }),
  );
}
document
  .querySelectorAll<HTMLButtonElement>("[data-follow]")
  .forEach(wireFollow);
const commentForm = $<HTMLFormElement>("[data-comment]");
commentForm?.addEventListener("submit", (ev) => {
  ev.preventDefault();
  busy($<HTMLButtonElement>("button", commentForm)!, async () => {
    const data = fields(commentForm);
    await api(
      `/api/stories/${encodeURIComponent(commentForm.dataset.comment!)}/comment`,
      "POST",
      {
        body: data.body,
        parent_id: data.parent_id ? Number(data.parent_id) : null,
      },
    );
    location.reload();
  });
});
for (const b of document.querySelectorAll<HTMLButtonElement>("[data-reply]"))
  b.addEventListener("click", () => {
    if (!commentForm) {
      toast("Verify your email to reply.");
      return;
    }
    $<HTMLInputElement>("input[name=parent_id]", commentForm)!.value =
      b.dataset.reply!;
    const el = $("#replying")!;
    el.textContent = "Replying to " + b.dataset.name;
    el.hidden = false;
    $<HTMLTextAreaElement>("textarea", commentForm)!.focus();
  });
for (const b of document.querySelectorAll<HTMLButtonElement>(
  "[data-edit-comment],[data-delete-comment],[data-report]",
))
  b.addEventListener("click", () =>
    busy(b, async () => {
      const id =
        b.dataset.editComment || b.dataset.deleteComment || b.dataset.report;
      if (b.dataset.deleteComment) {
        if (!confirm("Delete your comment?")) return;
        await api("/api/comments/" + id, "DELETE");
      } else if (b.dataset.editComment) {
        const body = await ask(
          "Edit your comment",
          $("[data-comment-body]", b.closest(".comment")!)!.textContent || "",
          "Comment",
        );
        if (body === null) return;
        await api("/api/comments/" + id, "PATCH", { body });
      } else {
        const reason = await ask(
          "Report this comment",
          "",
          "What’s the concern?",
        );
        if (reason === null) return;
        await api("/api/comments/" + id + "/report", "POST", { reason });
      }
      location.reload();
    }),
  );
$("[data-share]")?.addEventListener("click", async () => {
  try {
    if (navigator.share)
      await navigator.share({ title: document.title, url: location.href });
    else {
      await navigator.clipboard.writeText(location.href);
      toast("Story link copied.");
    }
  } catch (e) {
    if ((e as Error).name !== "AbortError")
      toast("Copy the address from your browser to share this story.");
  }
});
async function notifications() {
  if (!csrf || document.hidden) return;
  try {
    const list = await api("/api/notifications");
    const unread = list.filter((n: any) => !n.seen).length;
    const badge = $("#badge");
    if (badge) {
      badge.textContent = unread ? String(unread) : "";
      badge.setAttribute("aria-label", `${unread} unread notifications`);
    }
    const root = $("[data-notification-list]");
    if (root) {
      root.replaceChildren();
      for (const n of list) {
        const a = document.createElement("a");
        a.href = n.url;
        a.textContent = n.title + (n.seen ? "" : " · New");
        root.append(a);
      }
      if (!list.length)
        root.textContent =
          "You’re all caught up. Replies and new followed stories will appear here.";
    }
  } catch (e) {
    const root = $("[data-notification-list]");
    if (root) root.textContent = (e as Error).message;
  }
}
void notifications();
if (csrf) setInterval(notifications, 60000);
if ($("[data-follow-list]"))
  api("/api/follows")
    .then((list) => {
      const root = $("[data-follow-list]")!;
      root.replaceChildren();
      for (const f of list) {
        const div = document.createElement("p"),
          a = document.createElement("a"),
          b = document.createElement("button");
        a.href = `/${f.kind === "author" ? "authors" : "topics"}/${encodeURIComponent(f.target)}`;
        a.textContent = f.target;
        b.className = "button";
        b.dataset.follow = f.kind;
        b.dataset.target = f.target;
        b.setAttribute("aria-pressed", "true");
        b.textContent = "Following";
        wireFollow(b);
        div.append(a, " ", b);
        root.append(div);
      }
      if (!list.length) root.textContent = "You’re not following anyone yet.";
    })
    .catch((e) => ($("[data-follow-list]")!.textContent = e.message));
for (const b of document.querySelectorAll<HTMLButtonElement>(
  "[data-edit-author]",
))
  b.addEventListener("click", () => {
    const f = $<HTMLFormElement>("[data-admin-form=author]")!;
    for (const key of ["name", "bio", "avatar"])
      $<HTMLInputElement>(`[name=${key}]`, f)!.value =
        key === "name" ? b.dataset.editAuthor! : b.dataset[key] || "";
    f.scrollIntoView();
    $<HTMLInputElement>("[name=bio]", f)?.focus();
  });
function actionButton(label: string, fn: () => Promise<void>) {
  const b = document.createElement("button");
  b.className = "button";
  b.textContent = label;
  b.addEventListener("click", () => busy(b, fn));
  return b;
}
async function adminList(root: HTMLElement, query = "") {
  const kind = root.dataset.adminList!;
  try {
    const list = await api("/api/admin/" + kind + query);
    root.replaceChildren();
    if (!list.length) {
      root.textContent = "Nothing here yet.";
      return;
    }
    const wrap = document.createElement("div");
    wrap.className = "admin-table-wrap";
    const table = document.createElement("table"),
      head = document.createElement("thead"),
      tr = document.createElement("tr");
    const cols =
      kind === "members"
        ? ["name", "email", "role", "verified", "suspended"]
        : kind === "moderation"
          ? ["reason", "body", "name", "resolved"]
          : kind === "comments"
            ? ["name", "body", "hidden", "slug"]
            : ["recipient", "subject", "state", "attempts", "last_error"];
    for (const c of [...cols, "Actions"]) {
      const th = document.createElement("th");
      th.scope = "col";
      th.textContent = c.replaceAll("_", " ");
      tr.append(th);
    }
    head.append(tr);
    table.append(head);
    const tbody = document.createElement("tbody");
    for (const row of list) {
      const tr = document.createElement("tr");
      for (const c of cols) {
        const td = document.createElement("td");
        td.textContent = String(row[c] ?? "");
        tr.append(td);
      }
      const td = document.createElement("td");
      const act = async (action: string, body: unknown) => {
        await api("/api/admin/" + action, "POST", body);
        await adminList(root, query);
      };
      if (kind === "members") {
        td.append(
          actionButton(row.suspended ? "Restore" : "Suspend", () =>
            act("member", {
              id: row.id,
              role: row.role,
              suspended: !row.suspended,
            }),
          ),
          actionButton(
            row.role === "admin" ? "Make reader" : "Make admin",
            async () => {
              if (confirm(`Change ${row.name}’s role?`))
                await act("member", {
                  id: row.id,
                  role: row.role === "admin" ? "reader" : "admin",
                  suspended: row.suspended,
                });
            },
          ),
        );
      } else if (kind === "moderation") {
        td.append(
          actionButton(row.hidden ? "Show comment" : "Hide comment", () =>
            act("moderate", { id: row.comment_id, hidden: !row.hidden }),
          ),
        );
        if (!row.resolved)
          td.append(
            actionButton("Resolve report", () =>
              act("resolve", { id: row.id }),
            ),
          );
      } else if (kind === "comments") {
        td.append(
          actionButton(row.hidden ? "Show" : "Hide", () =>
            act("moderate", { id: row.id, hidden: !row.hidden }),
          ),
        );
      } else if (row.state === "failed")
        td.append(
          actionButton("Retry", () => act("retry-email", { id: row.id })),
        );
      tr.append(td);
      tbody.append(tr);
    }
    table.append(tbody);
    wrap.append(table);
    root.append(wrap);
  } catch (e) {
    root.textContent = (e as Error).message;
  }
}
document
  .querySelectorAll<HTMLElement>("[data-admin-list]")
  .forEach((root) => void adminList(root));
$<HTMLFormElement>("#member-search")?.addEventListener("submit", (e) => {
  e.preventDefault();
  void adminList(
    $("[data-admin-list=members]")!,
    "?q=" + encodeURIComponent($<HTMLInputElement>("#member-query")!.value),
  );
});
const ef = $<HTMLFormElement>("#editor-form");
if (ef) {
  const [{ Editor }, { default: StarterKit }, { default: Image }] =
    await Promise.all([
      import("@tiptap/core"),
      import("@tiptap/starter-kit"),
      import("@tiptap/extension-image"),
    ]);
  const el = $("#editor")!;
  let initial: any = {};
  try {
    initial = JSON.parse(el.dataset.initial || "{}");
  } catch {}
  let slug = ef.dataset.slug || "",
    changed = false,
    saving = false,
    saveAgain = false,
    timer: ReturnType<typeof setTimeout>,
    generation = 0;
  const content = initial.content || {
    type: "doc",
    content: (initial.body || "")
      .split("\n\n")
      .map((text: string) => ({
        type: "paragraph",
        content: text ? [{ type: "text", text }] : [],
      })),
  };
  const editor = new Editor({
    element: el,
    extensions: [
      StarterKit.configure({ heading: { levels: [2, 3, 4] } }),
      Image,
    ],
    content,
    editorProps: {
      attributes: {
        role: "textbox",
        "aria-label": "Article content",
        "aria-multiline": "true",
      },
    },
    onUpdate: () => {
      changed = true;
      generation++;
      clearTimeout(timer);
      timer = setTimeout(() => void save("draft", true), 1800);
    },
  });

  const syncFormats = () => {
    for (const b of ef.querySelectorAll<HTMLButtonElement>("[data-format]")) {
      const cmd = b.dataset.format!;
      if (
        [
          "bold",
          "italic",
          "heading",
          "bulletList",
          "orderedList",
          "blockquote",
        ].includes(cmd)
      ) {
        b.setAttribute(
          "aria-pressed",
          String(
            editor.isActive(cmd, cmd === "heading" ? { level: 2 } : undefined),
          ),
        );
      }
      if (cmd === "undo") b.disabled = !editor.can().undo();
    }
  };
  editor.on("transaction", syncFormats);
  syncFormats();
  if (initial.scheduled_at)
    $<HTMLInputElement>("[name=scheduled_at]", ef)!.value = new Date(
      initial.scheduled_at,
    )
      .toISOString()
      .slice(0, 16);
  ef.addEventListener("input", () => {
    changed = true;
    generation++;
    clearTimeout(timer);
    timer = setTimeout(() => void save("draft", true), 1800);
  });
  async function revisions() {
    if (!slug) return;
    try {
      const rows = await api(
        "/api/admin/revisions/" + encodeURIComponent(slug),
      );
      const root = $("[data-revisions]")!;
      root.replaceChildren();
      for (const row of rows) {
        root.append(
          actionButton(
            "Restore " + new Date(row.created_at).toLocaleString(),
            async () => {
              if (!confirm("Restore this version as a draft?")) return;
              await api(
                `/api/admin/revisions/${encodeURIComponent(slug)}/${row.id}`,
                "POST",
                {},
              );
              location.reload();
            },
          ),
        );
      }
      if (!rows.length) root.textContent = "No earlier versions yet.";
    } catch (e) {
      $("[data-revisions]")!.textContent = (e as Error).message;
    }
  }
  async function save(state: string, auto = false, schedule = false) {
    if (saving) {
      saveAgain = true;
      return;
    }
    saving = true;
    const version = generation;
    status(ef!, auto ? "Autosaving…" : "Saving…");
    const controls = Array.from(
      ef!.querySelectorAll<HTMLButtonElement>(
        ".editor-actions button,[data-schedule]",
      ),
    );
    controls.forEach((b) => (b.disabled = true));
    try {
      const data: any = fields(ef!);
      data.tags = (data.tags || "")
        .split(",")
        .map((t: string) => t.trim())
        .filter(Boolean);
      data.featured = $<HTMLInputElement>("[name=featured]", ef!)!.checked;
      data.content = editor.getJSON();
      data.status = state;
      if (auto && initial.status === "published") data.status = "published";
      delete data.scheduled_at;
      data.clear_schedule = !auto && !schedule;
      if (schedule) {
        const raw = $<HTMLInputElement>("[name=scheduled_at]", ef!)!.value;
        if (!raw) throw Error("Choose a date and time in UTC.");
        const at = new Date(raw + "Z");
        if (at.getTime() <= Date.now())
          throw Error("Choose a future publication time.");
        data.scheduled_at = at.toISOString();
        data.status = "draft";
      }
      const result = await api(
        slug ? "/admin/posts/" + encodeURIComponent(slug) : "/admin/posts",
        slug ? "PATCH" : "POST",
        data,
      );
      slug = result.slug;
      initial = result;
      changed = generation !== version;
      history.replaceState(null, "", "/admin/editor/" + slug);
      status(
        ef!,
        result.scheduled_at
          ? "Scheduled for " + new Date(result.scheduled_at).toUTCString()
          : result.status === "published"
            ? "Published."
            : "Draft saved.",
      );
      await revisions();
    } catch (e) {
      status(ef!, (e as Error).message, true);
    } finally {
      saving = false;
      controls.forEach((b) => (b.disabled = false));
      if (saveAgain) {
        saveAgain = false;
        void save("draft", true);
      }
    }
  }
  ef.addEventListener("submit", (e) => {
    e.preventDefault();
    clearTimeout(timer);
    void save("draft");
  });
  $("[data-publish]")?.addEventListener("click", () => {
    clearTimeout(timer);
    void save("published");
  });
  $("[data-unpublish]")?.addEventListener("click", () => {
    clearTimeout(timer);
    void save("draft");
  });
  $("[data-schedule]")?.addEventListener("click", () => {
    clearTimeout(timer);
    void save("draft", false, true);
  });
  $("[data-preview]")?.addEventListener("click", () => {
    const p = $("#preview")!;
    p.hidden = !p.hidden;
    if (!p.hidden) p.innerHTML = editor.getHTML();
  });
  for (const b of ef.querySelectorAll<HTMLButtonElement>("[data-format]"))
    b.addEventListener("click", async () => {
      const cmd = b.dataset.format!;
      const chain = editor.chain().focus();
      switch (cmd) {
        case "bold":
          chain.toggleBold().run();
          break;
        case "italic":
          chain.toggleItalic().run();
          break;
        case "heading":
          chain.toggleHeading({ level: 2 }).run();
          break;
        case "bulletList":
          chain.toggleBulletList().run();
          break;
        case "orderedList":
          chain.toggleOrderedList().run();
          break;
        case "blockquote":
          chain.toggleBlockquote().run();
          break;
        case "undo":
          chain.undo().run();
          break;
        case "link": {
          const href = await ask("Insert a link", "", "Full https:// URL");
          if (href) chain.setLink({ href }).run();
          break;
        }
        case "image": {
          const src = await ask(
            "Insert an image",
            "",
            "Image URL (upload first for a local image)",
          );
          if (src) {
            const alt = await ask("Describe the image", "", "Alternative text");
            chain.setImage({ src, alt: alt || "" }).run();
          }
          break;
        }
      }
    });
  $<HTMLInputElement>("#upload")!.addEventListener("change", async (e) => {
    const file = (e.target as HTMLInputElement).files?.[0];
    if (!file) return;
    try {
      const data = new FormData();
      data.append("image", file);
      const result = await api("/api/admin/uploads", "POST", data);
      $<HTMLInputElement>("[name=cover_image]", ef)!.value = result.url;
      changed = true;
      generation++;
      toast("Image uploaded. Its URL is ready in the cover field.");
    } catch (e) {
      toast((e as Error).message);
    }
  });
  window.addEventListener("beforeunload", (e) => {
    if (changed || saving) {
      e.preventDefault();
    }
  });
  void revisions();
}
// Old bookmark URLs used hashes; these now resolve to complete page flows.
const legacy: Record<string, string> = {
  "#editor": "/admin",
  "#console": "/search",
  "#topics": "/topics",
  "#posts": "/",
};
if (legacy[location.hash]) location.replace(legacy[location.hash]);
