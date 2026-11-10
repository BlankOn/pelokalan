// Client-side behaviour: dialogs, confirmations, toasts and the translation editor.
(function () {
    "use strict";

    // ---------- Generic helpers ----------

    document.addEventListener("click", function (ev) {
        var opener = ev.target.closest("[data-open-dialog]");
        if (opener) {
            var dlg = document.getElementById(opener.getAttribute("data-open-dialog"));
            if (dlg && dlg.showModal) {
                dlg.showModal();
                dlg.dispatchEvent(new CustomEvent("dialog-open"));
            }
            return;
        }
        var closer = ev.target.closest("[data-close-dialog]");
        if (closer) {
            var d = closer.closest("dialog");
            if (d) d.close();
        }
        // close open dropdown menus when clicking elsewhere
        document.querySelectorAll("details.dropdown[open], details.usermenu[open]").forEach(function (el) {
            if (!el.contains(ev.target)) el.removeAttribute("open");
        });
    });

    // The app's own popup for questions and messages; the browser's built-in
    // confirm/alert boxes are never used (they are blocked in some embedded browsers).
    // opts.ok: label of the confirm button; opts.danger: red button; opts.notice: OK only.
    function confirmDialog() {
        var d = document.getElementById("confirm-dialog");
        if (d) return d;
        var en = document.body.dataset.lang === "en";
        d = document.createElement("dialog");
        d.id = "confirm-dialog";
        d.className = "dialog confirm-dialog";
        d.dataset.deleteLabel = en ? "Delete" : "Hapus";
        d.innerHTML = '<p class="confirm-msg"></p><div class="dialog-actions">' +
            '<button type="button" class="btn" data-confirm-cancel></button>' +
            '<button type="button" class="btn btn-primary" data-confirm-ok></button></div>';
        d.querySelector("[data-confirm-cancel]").textContent = en ? "Cancel" : "Batal";
        d.querySelector("[data-confirm-ok]").textContent = en ? "Continue" : "Lanjutkan";
        document.body.appendChild(d);
        return d;
    }
    window.askConfirm = function (msg, opts) {
        opts = opts || {};
        var d = confirmDialog();
        var ok = d.querySelector("[data-confirm-ok]");
        var cancel = d.querySelector("[data-confirm-cancel]");
        if (!ok.dataset.label) ok.dataset.label = ok.textContent;
        cancel.hidden = !!opts.notice;
        if (!cancel.dataset.label) cancel.dataset.label = cancel.textContent;
        cancel.textContent = opts.cancel || cancel.dataset.label;
        d.querySelector(".confirm-msg").textContent = msg;
        ok.textContent = opts.ok || (opts.danger ? d.dataset.deleteLabel : opts.notice ? "OK" : ok.dataset.label);
        ok.classList.toggle("btn-danger", !!opts.danger);
        ok.classList.toggle("btn-primary", !opts.danger);
        return new Promise(function (resolve) {
            var done = function (val) {
                ok.removeEventListener("click", onOk);
                cancel.removeEventListener("click", onCancel);
                d.removeEventListener("close", onClose);
                if (d.open) d.close();
                resolve(val);
            };
            var onOk = function () { done(true); };
            var onCancel = function () { done(false); };
            var onClose = function () { done(false); };
            ok.addEventListener("click", onOk);
            cancel.addEventListener("click", onCancel);
            d.addEventListener("close", onClose);
            d.showModal();
            (opts.danger ? cancel : ok).focus();
        });
    };
    // showMessage: a notice with only an OK button
    window.showMessage = function (msg) { return window.askConfirm(msg, { notice: true }); };

    // hx-confirm: ask with the app's popup instead of the browser's
    document.addEventListener("htmx:confirm", function (ev) {
        var q = ev.detail.question;
        if (!q) return;
        ev.preventDefault();
        var elt = ev.detail.elt;
        var url = (elt.getAttribute("hx-post") || elt.getAttribute("hx-delete") || "");
        window.askConfirm(q, {
            ok: elt.getAttribute("data-confirm-ok"),
            danger: /\/(delete|revoke|remove)[^/]*$/.test(url) || elt.hasAttribute("hx-delete")
        }).then(function (yes) {
            if (yes) ev.detail.issueRequest(true);
        });
    });

    document.addEventListener("submit", function (ev) {
        var form = ev.target;
        var msg = form.getAttribute && form.getAttribute("data-confirm");
        if (!msg) return;
        if (form._confirmed) { form._confirmed = false; return; }
        ev.preventDefault();
        ev.stopImmediatePropagation();
        var submitter = ev.submitter;
        var action = form.getAttribute("action") || "";
        window.askConfirm(msg, {
            ok: form.getAttribute("data-confirm-ok"),
            danger: /\/(delete|revoke|remove)[^/]*$/.test(action) || form.hasAttribute("data-confirm-danger")
        }).then(function (yes) {
            if (!yes) return;
            form._confirmed = true;
            if (form.requestSubmit) form.requestSubmit(submitter && submitter.form === form ? submitter : undefined);
            else form.submit();
        });
    }, true);

    document.addEventListener("change", function (ev) {
        var el = ev.target;
        if (el.hasAttribute && el.hasAttribute("data-autosubmit") && el.form) {
            el.form.requestSubmit ? el.form.requestSubmit() : el.form.submit();
        }
        var toggle = el.getAttribute && el.getAttribute("data-other-toggle");
        if (toggle) {
            var other = document.getElementById(toggle);
            if (other) {
                other.hidden = el.value !== "other";
                other.required = el.value === "other";
            }
        }
    });

    document.querySelectorAll("dialog[data-open-on-load]").forEach(function (d) {
        if (d.showModal) d.showModal();
    });

    // Remove toasts after a few seconds
    var toastArea = document.getElementById("toast");
    if (toastArea) {
        new MutationObserver(function () {
            toastArea.querySelectorAll(".toast:not([data-timer])").forEach(function (t) {
                t.setAttribute("data-timer", "1");
                setTimeout(function () { t.remove(); }, 4500);
            });
        }).observe(toastArea, { childList: true });
    }

    // Sinkronkan (project files): start the sync, spin until it is done, then report and refresh the list
    document.addEventListener("submit", function (ev) {
        var form = ev.target;
        if (!form.hasAttribute || !form.hasAttribute("data-sync")) return;
        ev.preventDefault();
        if (form.classList.contains("is-syncing")) return;
        var btn = form.querySelector("button");
        var label = form.querySelector(".sync-label");
        var msg = document.querySelector(".pf-sync-msg");
        var oldLabel = label.textContent;
        var show = function (text, isError) {
            if (!msg) return;
            msg.hidden = !text;
            msg.textContent = text || "";
            msg.classList.toggle("c-err", !!isError);
        };
        var finish = function () {
            form.classList.remove("is-syncing");
            btn.disabled = false;
            label.textContent = oldLabel;
        };
        form.classList.add("is-syncing");
        btn.disabled = true;
        label.textContent = form.getAttribute("data-syncing") || oldLabel;
        show("");
        fetch(form.action, { method: "POST", headers: { "X-Sync": "1" }, credentials: "same-origin" })
            .then(function (r) { return r.json(); })
            .then(function (res) {
                if (res.error) { show(res.error, true); finish(); return; }
                var poll = function () {
                    fetch(form.action, { headers: { "Accept": "application/json" }, credentials: "same-origin", cache: "no-store" })
                        .then(function (r) { return r.json(); })
                        .then(function (st) {
                            if (st.running) {
                                if (st.message) label.textContent = st.message;
                                setTimeout(poll, 2000);
                                return;
                            }
                            finish();
                            var tm = document.querySelector(".pf-synced time");
                            if (tm && st.last) {
                                tm.setAttribute("datetime", st.last);
                                if (window.formatLocalTime) window.formatLocalTime(tm);
                                var ago = document.querySelector(".pf-synced-ago");
                                if (ago && st.last_ago) ago.textContent = st.last_ago;
                            }
                            var text = [st.message, st.error].filter(Boolean).join(" ");
                            show(text, !!st.error);
                            var rows = document.getElementById("project-file-rows");
                            if (st.changed && rows && window.htmx) {
                                var q = document.querySelector(".pf-toolbar input[name=q]");
                                htmx.ajax("GET", form.action.replace(/\/sync$/, "") + "?q=" + encodeURIComponent(q ? q.value : ""),
                                    { target: rows, swap: "innerHTML" });
                            }
                        })
                        .catch(function () { setTimeout(poll, 4000); });
                };
                setTimeout(poll, 1500);
            })
            .catch(function () { show(oldLabel + ": error", true); finish(); });
    });

    // ---------- Editor ----------

    var app = document.getElementById("editor-app");
    if (!app) return;

    var catalogId = app.dataset.catalog;
    var readOnly = app.dataset.readonly === "true";
    var currentId = null;
    var dirty = false;
    var listEl = function () { return document.getElementById("entry-list"); };
    var form = function () { return document.getElementById("entry-form"); };

    function rows() { return Array.prototype.slice.call(listEl().querySelectorAll(".entry-row")); }

    function markSelected() {
        rows().forEach(function (r) { r.classList.toggle("selected", r.dataset.id === currentId); });
        document.getElementById("filter-selected").value = currentId || "";
    }

    function scrollToRow(row) {
        var list = listEl();
        var top = row.offsetTop - list.offsetTop;
        if (top < list.scrollTop) list.scrollTop = top;
        else if (top + row.offsetHeight > list.scrollTop + list.clientHeight)
            list.scrollTop = top + row.offsetHeight - list.clientHeight;
    }

    function setDirty(v) {
        dirty = v;
        var f = form();
        if (f) f.classList.toggle("dirty", v);
    }

    // ----- Loading and saving entries -----
    // Plain fetch instead of htmx so a stalled request can never block the
    // editor: a new load cancels the previous one, every request times out,
    // and navigation requested during a save waits for that save.
    var inflight = null;     // AbortController of the current request
    var saving = false;
    var afterSave = null;    // entry to open once the running save finishes

    function setBusy(v) {
        var ed = document.getElementById("editor");
        if (ed) ed.classList.toggle("loading", v);
    }

    function request(url, opts) {
        if (inflight) inflight.abort();
        var ctl = new AbortController();
        inflight = ctl;
        var timer = setTimeout(function () { ctl.abort(); }, 20000);
        opts = opts || {};
        opts.signal = ctl.signal;
        opts.credentials = "same-origin";
        opts.headers = { "HX-Request": "true" };
        setBusy(true);
        return fetch(url, opts).then(function (resp) {
            return resp.text().then(function (html) {
                return {
                    ok: resp.ok, status: resp.status, html: html,
                    retarget: resp.headers.get("HX-Retarget"), redirect: resp.headers.get("HX-Redirect")
                };
            });
        }).finally(function () {
            clearTimeout(timer);
            if (inflight === ctl) {
                inflight = null;
                setBusy(false);
            }
        });
    }

    window.toast = function (m, e) { toast(m, e); };
    function toast(message, isError) {
        var area = document.getElementById("toast");
        if (!area) return;
        var t = document.createElement("div");
        t.className = "toast" + (isError ? " toast-error" : "");
        t.textContent = message;
        area.appendChild(t);
    }

    // Applies an editor response: the #editor pane plus out-of-band parts
    // (sidebar, saved row, status bar).
    function applyEditor(html) {
        var tpl = document.createElement("template");
        tpl.innerHTML = html.trim();
        Array.prototype.slice.call(tpl.content.children).forEach(function (el) {
            var target = el.id && document.getElementById(el.id);
            if (!target) return;
            if (el.hasAttribute("hx-swap-oob")) el.removeAttribute("hx-swap-oob");
            else if (el.id !== "editor") return;
            target.replaceWith(el);
        });
        var ed = document.getElementById("editor");
        if (window.htmx && ed) htmx.process(ed);
        undoStack = [];
        lastTyping = 0;
        applyLock();
        currentId = (ed && ed.dataset.entryId) || null;
        setDirty(false);
        markSelected();
        var row = currentId && document.getElementById("row-" + currentId);
        if (row) scrollToRow(row);
                if (ed && !ed.querySelector(".ed-alert-error")) focusTranslation();
        if (window.loadThreads) window.loadThreads(document.getElementById("sidebar"));
    }

    // Handles a response; returns true when the editor was updated
    function handle(r) {
        if (r.redirect) {
            window.location.href = r.redirect;
            return false;
        }
        if (r.ok) {
            applyEditor(r.html);
            return true;
        }
        if (r.retarget === "#toast") {
            var area = document.getElementById("toast");
            if (area) area.insertAdjacentHTML("beforeend", r.html);
        } else {
            toast(app.dataset.msgError + " (" + r.status + ")", true);
        }
        return false;
    }

    function load(id) {
        request("/catalogs/" + catalogId + "/entries/" + id).then(handle).catch(function (err) {
            if (err && err.name === "AbortError" && inflight) return; // replaced by a newer load
            toast(app.dataset.msgError, true);
        });
    }

    function save(nextId) {
        var f = form();
        if (!f || readOnly) return;
        // while a pre-translation job runs the editor is read-only: just move on
        if (editorLocked()) { if (nextId) load(nextId); return; }
        if (saving) { // the running save will move on afterwards
            afterSave = nextId;
            return;
        }
        f.querySelector("input[name=next_id]").value = nextId || "";
        var body = new URLSearchParams(new FormData(f));
        saving = true;
        afterSave = null;
        request(f.getAttribute("action"), { method: "POST", body: body }).then(function (r) {
            saving = false;
            if (handle(r)) {
                if (afterSave && afterSave !== currentId) load(afterSave);
                return;
            }
            saveFailed(afterSave || nextId);
        }).catch(function () {
            saving = false;
            toast(app.dataset.msgError, true);
            saveFailed(afterSave || nextId);
        });
    }

    // After a failed save the text stays in the editor. Moving on is the
    // translator's choice, so the editor never gets stuck on one entry.
    function saveFailed(nextId) {
        if (!nextId || nextId === currentId) return;
        window.askConfirm(app.dataset.msgSaveFailed).then(function (yes) {
            if (!yes) return;
            setDirty(false);
            load(nextId);
        });
    }

    // Load an entry; saves the current one first when it has unsaved changes.
    function select(id) {
        if (!id) return;
        if (saving) {
            afterSave = id;
            return;
        }
        if (id === currentId && !dirty) return;
        if (dirty && form() && !readOnly) {
            save(id);
            return;
        }
        load(id);
    }

    function neighbour(offset) {
        var all = rows();
        var idx = all.findIndex(function (r) { return r.dataset.id === currentId; });
        var n = all[idx + offset];
        return n ? n.dataset.id : null;
    }

    function saveAndNext() {
        if (!form() || readOnly) return;
        save(neighbour(1));
    }

    // "Save & next" button and Enter-submits go through the same path
    document.addEventListener("submit", function (ev) {
        if (ev.target.id !== "entry-form") return;
        ev.preventDefault();
        saveAndNext();
    });

    function activePanel() {
        var f = form();
        if (!f) return null;
        return f.querySelector(".tr-input:not([hidden])");
    }

    function focusTranslation() {
        var ta = activePanel();
        if (ta) {
            ta.focus({ preventScroll: true });
            ta.setSelectionRange(ta.value.length, ta.value.length);
        }
    }

    // The orange "clear needs work, save & next" button is only shown while the
    // needs-work box is checked.
    function syncUnfuzzyButton() {
        var f = form();
        var btn = f && f.querySelector("#btn-unfuzzy-save");
        var cb = f && f.querySelector("input[name=fuzzy]");
        if (btn && cb) {
            btn.hidden = !cb.checked;
            var save = f.querySelector("#btn-save");
            if (save) save.title = cb.checked ? "Ctrl+Shift+Enter" : "Ctrl+Enter";
        }
    }

    // Pre-translation job running (its banner has class ai-running): the editor is read-only
    // until it finishes, then the list and the current entry are reloaded to show the results.
    var wasLocked = false;
    function editorLocked() { return !!document.querySelector("#ai-job.ai-running"); }
    function applyLock() {
        var locked = editorLocked(), f = form();
        app.classList.toggle("editor-locked", locked);
        if (f) {
            f.querySelectorAll(".tr-input").forEach(function (ta) { ta.readOnly = locked || ta.classList.contains("ai-filling"); });
            f.querySelectorAll("button, input[type=checkbox]").forEach(function (b) {
                if (locked) {
                    if (!b.disabled) { b.dataset.lockDisabled = "1"; b.disabled = true; }
                } else if (b.dataset.lockDisabled) {
                    b.disabled = false;
                    delete b.dataset.lockDisabled;
                }
            });
            var note = f.querySelector(".ed-locked-note");
            if (locked && !note) {
                note = document.createElement("div");
                note.className = "ed-alert ed-locked-note";
                note.textContent = app.dataset.lockedMsg || "";
                f.insertBefore(note, f.firstChild);
            } else if (!locked && note) {
                note.remove();
            }
        }
        if (wasLocked && !locked) {
            // the job finished: show what it changed
            var ff = document.getElementById("filter-form");
            if (ff && window.htmx) htmx.trigger(ff, "submit");
            if (currentId) load(currentId);
        }
        wasLocked = locked;
    }
    // the job banner replaces itself (outerHTML), so listen for new content (htmx:load bubbles
    // from the inserted element) and also check once a second in case an event is missed
    document.body.addEventListener("htmx:load", function () { setTimeout(applyLock, 0); });
    setInterval(function () { if (editorLocked() !== wasLocked) applyLock(); }, 1000);

    // AI translation of the current entry: the result goes into the translation box (unsaved)
    var aiBusy = false;
    function aiTranslateEntry() {
        var f = form(), btn = f && f.querySelector("#btn-ai-entry");
        if (!btn || readOnly || aiBusy || editorLocked()) return;
        var body = new URLSearchParams();
        var boxes = Array.prototype.slice.call(f.querySelectorAll(".tr-input"));
        boxes.forEach(function (ta) { body.set("msgstr" + ta.dataset.panel, ta.value); });
        var label = btn.querySelector("span"), old = label.textContent;
        aiBusy = true;
        btn.classList.add("working");
        btn.disabled = true;
        // the translation box is locked (and shimmers) until the answer arrives
        boxes.forEach(function (ta) { ta.readOnly = true; ta.classList.add("ai-filling"); });
        label.textContent = app.dataset.aiWorking || "…";
        var entryId = currentId;
        fetch(btn.dataset.url, { method: "POST", body: body, credentials: "same-origin" })
            .then(function (r) { return r.json(); })
            .then(function (res) {
                if (currentId !== entryId) return; // the user moved on meanwhile
                if (res.error) { toast(res.error, true); return; }
                pushUndo();
                var beforeForms = boxes.map(function (ta) { return ta.value; });
                boxes.forEach(function (ta) {
                    var i = parseInt(ta.dataset.panel, 10);
                    if (res.forms && res.forms[i] !== undefined) ta.value = res.forms[i];
                });
                setDirty(true);
                var cb = f.querySelector("input[name=fuzzy]");
                if (cb) {
                    if (res.flagged) cb.checked = true;
                    else clearFuzzyOnEdit();
                    cb.dataset.touched = res.flagged ? "1" : cb.dataset.touched || "";
                }
                syncUnfuzzyButton();
                var notice = f.querySelector(".ed-notice");
                if (notice) notice.textContent = res.notice || "";
                var changed = boxes.some(function (ta, i) { return ta.value !== beforeForms[i]; });
                if (changed && beforeForms.some(function (v) { return v.trim() !== ""; })) {
                    setReplaced(beforeForms);
                    showCompare();
                }
                focusTranslation();
            })
            .catch(function () { toast(app.dataset.msgError || "Error", true); })
            .finally(function () {
                aiBusy = false;
                btn.classList.remove("working");
                btn.disabled = false;
                label.textContent = old;
                boxes.forEach(function (ta) { ta.classList.remove("ai-filling"); ta.readOnly = editorLocked(); });
            });
    }
    document.addEventListener("click", function (ev) {
        if (ev.target.closest && ev.target.closest("#btn-ai-entry")) { ev.preventDefault(); aiTranslateEntry(); }
    });

    function unfuzzyShown() {
        var btn = form() && form().querySelector("#btn-unfuzzy-save");
        return !!(btn && !btn.hidden);
    }

    function unfuzzySaveAndNext() {
        var cb = form() && form().querySelector("input[name=fuzzy]");
        if (!cb || readOnly) return;
        cb.checked = false;
        cb.dataset.touched = "1";
        setDirty(true);
        syncUnfuzzyButton();
        saveAndNext();
    }

    document.addEventListener("click", function (ev) {
        if (ev.target.closest("#btn-unfuzzy-save")) {
            ev.preventDefault();
            unfuzzySaveAndNext();
        }
    });

    // Undo for the translation box: snapshots of every plural form and the needs-work flag.
    // Typing is grouped into bursts; tools (copy source, AI, glossary, thesaurus, suggestions)
    // record a snapshot before they change the text. The history resets per entry.
    var undoStack = [], lastTyping = 0;
    function trSnapshot() {
        var f = form();
        if (!f) return null;
        var cb = f.querySelector("input[name=fuzzy]");
        return {
            values: Array.prototype.map.call(f.querySelectorAll(".tr-input"), function (ta) { return ta.value; }),
            fuzzy: cb ? cb.checked : false
        };
    }
    function sameSnapshot(a, b) {
        return a && b && a.fuzzy === b.fuzzy && a.values.join("\u0000") === b.values.join("\u0000");
    }
    function updateUndoButton() {
        var f = form(), btn = f && f.querySelector("#btn-undo");
        if (btn) btn.disabled = undoStack.length === 0;
    }
    function pushUndo() {
        var s = trSnapshot();
        if (!s || sameSnapshot(undoStack[undoStack.length - 1], s)) return;
        undoStack.push(s);
        if (undoStack.length > 50) undoStack.shift();
        updateUndoButton();
    }
    window.trBeforeChange = pushUndo; // used by the thesaurus popup

    // Compare the previous translation (replaced by AI) with the current text, word by word
    function diffWords(a, b) {
        var x = a.split(/(\s+)/), y = b.split(/(\s+)/), n = x.length, m = y.length;
        var L = [];
        for (var i = 0; i <= n; i++) { L.push(new Array(m + 1).fill(0)); }
        for (i = n - 1; i >= 0; i--) for (var j = m - 1; j >= 0; j--)
            L[i][j] = x[i] === y[j] ? L[i + 1][j + 1] + 1 : Math.max(L[i + 1][j], L[i][j + 1]);
        var before = [], after = [];
        i = 0; j = 0;
        while (i < n && j < m) {
            if (x[i] === y[j]) { before.push(["", x[i]]); after.push(["", y[j]]); i++; j++; }
            else if (L[i + 1][j] >= L[i][j + 1]) { before.push(["del", x[i++]]); }
            else { after.push(["ins", y[j++]]); }
        }
        while (i < n) before.push(["del", x[i++]]);
        while (j < m) after.push(["ins", y[j++]]);
        return [before, after];
    }
    function renderDiff(el, parts) {
        el.innerHTML = "";
        parts.forEach(function (p) {
            var node = p[0] && p[1].trim() ? document.createElement(p[0]) : document.createTextNode(p[1]);
            if (p[0] && p[1].trim()) node.textContent = p[1];
            el.appendChild(node);
        });
    }
    function replacedForms() {
        var btn = form() && form().querySelector("#btn-compare");
        if (!btn || !btn.dataset.replaced) return null;
        try { return JSON.parse(btn.dataset.replaced); } catch (e) { return null; }
    }
    function showCompare() {
        var f = form(), panel = f && f.querySelector("#cmp-panel"), forms = replacedForms(), ta = activePanel();
        if (!panel || !forms || !ta) return;
        var i = parseInt(ta.dataset.panel, 10) || 0;
        var d = diffWords(forms[i] || "", ta.value);
        renderDiff(panel.querySelector(".cmp-before"), d[0]);
        renderDiff(panel.querySelector(".cmp-after"), d[1]);
        panel.hidden = false;
        f.querySelector("#btn-compare").classList.add("active");
    }
    function hideCompare() {
        var f = form(), panel = f && f.querySelector("#cmp-panel");
        if (panel) panel.hidden = true;
        var btn = f && f.querySelector("#btn-compare");
        if (btn) btn.classList.remove("active");
    }
    // the editor's own AI button reports what it replaced, so the comparison works before saving
    function setReplaced(forms) {
        var btn = form() && form().querySelector("#btn-compare");
        if (!btn) return;
        btn.dataset.replaced = JSON.stringify(forms);
        btn.hidden = false;
    }
    document.addEventListener("click", function (ev) {
        if (!ev.target.closest) return;
        if (ev.target.closest("#btn-compare")) {
            ev.preventDefault();
            var panel = form() && form().querySelector("#cmp-panel");
            if (panel && !panel.hidden) hideCompare(); else showCompare();
        } else if (ev.target.closest("#cmp-close")) {
            hideCompare();
        } else if (ev.target.closest("#cmp-use-before")) {
            var forms = replacedForms(), f = form();
            if (!forms || !f || editorLocked()) return;
            pushUndo();
            f.querySelectorAll(".tr-input").forEach(function (ta) {
                var i = parseInt(ta.dataset.panel, 10);
                if (forms[i] !== undefined) ta.value = forms[i];
            });
            setDirty(true);
            showCompare();
        }
    });
    // keep the comparison current while typing
    document.addEventListener("input", function (ev) {
        var panel = form() && form().querySelector("#cmp-panel");
        if (panel && !panel.hidden && ev.target.matches && ev.target.matches("#entry-form .tr-input")) showCompare();
    });
    function undo() {
        var f = form(), s = undoStack.pop();
        if (!f || !s) return;
        f.querySelectorAll(".tr-input").forEach(function (ta, i) { if (s.values[i] !== undefined) ta.value = s.values[i]; });
        var cb = f.querySelector("input[name=fuzzy]");
        if (cb) { cb.checked = s.fuzzy; cb.dataset.touched = "1"; }
        syncUnfuzzyButton();
        setDirty(true);
        lastTyping = 0;
        updateUndoButton();
        focusTranslation();
    }
    document.addEventListener("beforeinput", function (ev) {
        if (!ev.target.matches || !ev.target.matches("#entry-form .tr-input")) return;
        var now = Date.now();
        if (now - lastTyping > 1200) pushUndo(); // a new burst of typing
        lastTyping = now;
    });
    document.addEventListener("click", function (ev) {
        if (ev.target.closest && ev.target.closest("#btn-undo")) { ev.preventDefault(); undo(); }
    });

    // Glossary chips insert their translation at the cursor of the translation box
    document.addEventListener("click", function (ev) {
        var btn = ev.target.closest && ev.target.closest("#entry-form .gl-insert[data-insert]");
        if (!btn || readOnly) return;
        var ta = activePanel();
        if (!ta || ta.readOnly) return;
        pushUndo();
        ta.setRangeText(btn.getAttribute("data-insert"), ta.selectionStart, ta.selectionEnd, "end");
        ta.focus();
        clearFuzzyOnEdit();
        setDirty(true);
    });

    function clearFuzzyOnEdit() {
        var cb = form() && form().querySelector("input[name=fuzzy]");
        if (cb && cb.checked && !cb.dataset.touched) { cb.checked = false; syncUnfuzzyButton(); }
    }

    function copySource() {
        var f = form();
        if (!f) return;
        pushUndo();
        f.querySelectorAll(".tr-input").forEach(function (ta) {
            var idx = parseInt(ta.dataset.panel, 10);
            var src = f.querySelector('.src-text[data-src="' + Math.min(idx, 1) + '"]');
            if (src) ta.value = src.textContent;
        });
        setDirty(true);
        focusTranslation();
    }

    function useSuggestion(index) {
        var s = document.querySelector('.suggestion[data-index="' + index + '"]');
        var ta = activePanel();
        if (s && ta) {
            pushUndo();
            ta.value = s.dataset.text;
            clearFuzzyOnEdit();
            setDirty(true);
            focusTranslation();
        }
    }

    listEl().parentElement.addEventListener("click", function (ev) {
        var row = ev.target.closest(".entry-row");
        if (row) select(row.dataset.id);
    });

    document.getElementById("sidebar").addEventListener("click", function (ev) {
        var s = ev.target.closest(".suggestion");
        if (s) useSuggestion(s.dataset.index);
    });

    document.addEventListener("click", function (ev) {
        if (ev.target.closest("#btn-copy-source")) copySource();
        var tab = ev.target.closest(".ptab");
        if (tab) {
            var f = form();
            f.querySelectorAll(".ptab").forEach(function (t) { t.classList.toggle("active", t === tab); });
            f.querySelectorAll(".tr-input").forEach(function (ta) { ta.hidden = ta.dataset.panel !== tab.dataset.tab; });
            focusTranslation();
        }
        var filterLink = ev.target.closest("[data-filter-status]");
        if (filterLink) {
            ev.preventDefault();
            setFilter(filterLink.dataset.filterStatus);
        }
    });

    // status filter: a dropdown of checkboxes (an entry shows when it matches any ticked one)
    function filterBoxes() {
        var box = document.getElementById("filter-status");
        return box ? Array.prototype.slice.call(box.querySelectorAll("input[name=status]")) : [];
    }
    function updateFilterLabel() {
        var box = document.getElementById("filter-status");
        if (!box) return;
        var label = box.querySelector(".mf-label");
        var on = filterBoxes().filter(function (cb) { return cb.checked; });
        if (!on.length) label.textContent = label.dataset.all;
        else if (on.length === 1) label.textContent = on[0].parentNode.textContent.trim();
        else label.textContent = label.dataset.many.replace("%d", on.length);
        box.classList.toggle("mf-active", on.length > 0);
    }
    // shortcuts show exactly these filters: status bar counts tick one, Validasi ticks
    // everything that still needs work (problems, untranslated, needs review)
    function setFilter(status) {
        var want = [].concat(status), boxes = filterBoxes(), target = null;
        boxes.forEach(function (cb) { cb.checked = want.indexOf(cb.value) >= 0; if (cb.checked && !target) target = cb; });
        updateFilterLabel();
        (target || boxes[0]).dispatchEvent(new Event("change", { bubbles: true }));
    }
    document.addEventListener("change", function (ev) {
        if (ev.target.matches && ev.target.matches("#filter-status input[name=status]")) updateFilterLabel();
    });
    document.addEventListener("click", function (ev) {
        var box = document.getElementById("filter-status");
        if (!box) return;
        if (ev.target.closest(".mf-clear")) {
            var boxes = filterBoxes();
            boxes.forEach(function (cb) { cb.checked = false; });
            updateFilterLabel();
            box.open = false;
            if (boxes[0]) boxes[0].dispatchEvent(new Event("change", { bubbles: true }));
        } else if (box.open && !box.contains(ev.target)) {
            box.open = false;
        }
    });
    document.addEventListener("keydown", function (ev) {
        var box = document.getElementById("filter-status");
        if (ev.key === "Escape" && box && box.open) { box.open = false; box.querySelector("summary").focus(); }
    });
    updateFilterLabel();

    // Downloading a file that still has problem entries (e.g. placeholders msgfmt -c would
    // reject): ask first, or show those entries
    document.addEventListener("click", function (ev) {
        var link = ev.target.closest && ev.target.closest("a[data-check-issues]");
        if (!link) return;
        var count = document.querySelector("[data-issue-count]");
        var n = count ? parseInt(count.dataset.issueCount, 10) || 0 : 0;
        if (!n || link.dataset.checked === "1") { link.dataset.checked = ""; return; }
        ev.preventDefault();
        window.askConfirm(link.dataset.checkIssues.replace("%d", n), { ok: link.dataset.checkOk, cancel: link.dataset.checkShow }).then(function (yes) {
            if (yes) { link.dataset.checked = "1"; link.click(); }
            else setFilter("issues");
        });
    });

    var validate = document.getElementById("btn-validate");
    if (validate) validate.addEventListener("click", function () { setFilter(["issues", "untranslated", "fuzzy"]); });

    document.addEventListener("input", function (ev) {
        if (ev.target.closest("#entry-form") || ev.target.matches(".comment-input")) {
            if (ev.target.matches(".tr-input")) clearFuzzyOnEdit();
            if (ev.target.matches("input[name=fuzzy]")) ev.target.dataset.touched = "1";
            setDirty(true);
        }
    });
    document.addEventListener("change", function (ev) {
        if (ev.target.matches("#entry-form input[name=fuzzy]")) {
            ev.target.dataset.touched = "1";
            setDirty(true);
            syncUnfuzzyButton();
        }
    });

    // Plain Up/Down move between entries, also from inside the translation box
    // (Left/Right keep moving the cursor). In a multi-line translation the arrows
    // still move between lines and only leave the entry from the first/last line.
    function arrowNavigates(ev) {
        if (ev.isComposing || document.querySelector("dialog[open]")) return false;
        if (!document.getElementById("entry-list")) return false;
        var t = ev.target;
        if (t.matches && t.matches("#entry-form .tr-input")) {
            if (t.selectionStart !== t.selectionEnd) return false;
            var v = t.value, pos = t.selectionStart;
            if (ev.key === "ArrowUp") return v.lastIndexOf("\n", pos - 1) < 0;
            return v.indexOf("\n", pos) < 0;
        }
        // other text fields, menus and buttons that use arrows keep them
        if (t.isContentEditable || (t.matches && t.matches("input, textarea, select, [role=tab], [role=menu] *"))) return false;
        return true;
    }

    document.addEventListener("keydown", function (ev) {
        var mod = ev.ctrlKey || ev.metaKey;
        if (!mod && !ev.altKey && !ev.shiftKey && (ev.key === "ArrowDown" || ev.key === "ArrowUp")) {
            if (arrowNavigates(ev)) {
                ev.preventDefault();
                select(neighbour(ev.key === "ArrowDown" ? 1 : -1));
            }
            return;
        }
        if (!mod) return;
        // editing shortcuts are off while the editor is locked; navigation keeps working
        if (editorLocked() && /^(Enter|u|U|b|B|a|A)$/.test(ev.key)) { ev.preventDefault(); return; }
        switch (ev.key) {
            case "Enter":
                ev.preventDefault();
                // Ctrl+Enter presses the main save button: "clear needs work, save & next" when it
                // is shown, else "save & next". Ctrl+Shift+Enter always saves keeping the flag.
                if (!ev.shiftKey && unfuzzyShown()) unfuzzySaveAndNext(); else saveAndNext();
                break;
            case "ArrowDown":
                ev.preventDefault();
                select(neighbour(1));
                break;
            case "ArrowUp":
                ev.preventDefault();
                select(neighbour(-1));
                break;
            case "a": case "A":
                if (ev.shiftKey && form() && form().querySelector("#btn-ai-entry")) {
                    ev.preventDefault();
                    aiTranslateEntry();
                }
                break;
            case "u": case "U": {
                var cb = form() && form().querySelector("input[name=fuzzy]");
                if (cb) {
                    ev.preventDefault();
                    cb.checked = !cb.checked;
                    cb.dataset.touched = "1";
                    setDirty(true);
                    syncUnfuzzyButton();
                }
                break;
            }
            case "b": case "B":
                if (!readOnly) { ev.preventDefault(); copySource(); }
                break;
            case "f": case "F":
                ev.preventDefault();
                document.getElementById("filter-q").focus();
                break;
            default:
                if (/^[1-5]$/.test(ev.key) && !readOnly) {
                    ev.preventDefault();
                    useSuggestion(ev.key);
                }
        }
    });

    document.body.addEventListener("htmx:afterSettle", function (ev) {
        var target = ev.detail.target || ev.target;
        if (target && target.id === "entry-list") {
            markSelected();
            if (!currentId || !document.getElementById("row-" + currentId)) {
                var first = rows()[0];
                if (first && !dirty) select(first.dataset.id);
            }
        }
    });

    // Keep the selection highlight on rows replaced out-of-band after a save
    document.body.addEventListener("htmx:oobAfterSwap", markSelected);

    document.body.addEventListener("htmx:responseError", function (ev) {
        var area = document.getElementById("toast");
        if (area && !ev.detail.xhr.getResponseHeader("HX-Retarget")) {
            var t = document.createElement("div");
            t.className = "toast toast-error";
            t.textContent = ev.detail.xhr.status + " " + ev.detail.xhr.statusText;
            area.appendChild(t);
        }
    });

    window.addEventListener("beforeunload", function (ev) {
        if (dirty) {
            ev.preventDefault();
            ev.returnValue = app.dataset.msgUnsaved;
            return app.dataset.msgUnsaved;
        }
    });

    // Resizable split between the list and the editor
    var splitter = document.getElementById("splitter");
    var mainCol = document.getElementById("main-col");
    try {
        var saved = localStorage.getItem("listHeight");
        if (saved) document.documentElement.style.setProperty("--list-height", saved);
    } catch (e) { /* storage unavailable */ }
    function setListHeight(pct) {
        pct = Math.min(85, Math.max(15, pct));
        document.documentElement.style.setProperty("--list-height", pct + "%");
        return pct;
    }
    function saveListHeight() {
        try {
            localStorage.setItem("listHeight", getComputedStyle(document.documentElement).getPropertyValue("--list-height").trim());
        } catch (e) { /* storage unavailable */ }
    }
    function pctAt(clientY) {
        var rect = mainCol.getBoundingClientRect();
        var header = mainCol.querySelector(".list-header").offsetHeight;
        return (clientY - rect.top - header) / (rect.height - header) * 100;
    }
    // pointer events cover mouse, pen and touch (fingers drag the grip on phones)
    splitter.addEventListener("pointerdown", function (ev) {
        if (ev.button !== 0) return;
        ev.preventDefault();
        // capture keeps the moves coming when the finger leaves the thin strip; the
        // document listeners below are the fallback when capture is not available
        try { splitter.setPointerCapture(ev.pointerId); } catch (e) { /* not capturable */ }
        splitter.classList.add("dragging");
        function move(e) { setListHeight(pctAt(e.clientY)); }
        function up() {
            splitter.classList.remove("dragging");
            document.removeEventListener("pointermove", move);
            document.removeEventListener("pointerup", up);
            document.removeEventListener("pointercancel", up);
            saveListHeight();
        }
        document.addEventListener("pointermove", move);
        document.addEventListener("pointerup", up);
        document.addEventListener("pointercancel", up);
    });
    // keyboard: arrow keys move the divider
    splitter.addEventListener("keydown", function (ev) {
        if (ev.key !== "ArrowUp" && ev.key !== "ArrowDown") return;
        ev.preventDefault();
        ev.stopPropagation();
        var rect = splitter.getBoundingClientRect();
        setListHeight(pctAt(rect.top + rect.height / 2 + (ev.key === "ArrowUp" ? -24 : 24)));
        saveListHeight();
    });

        // open the entry named in the URL (?entry=…), otherwise the first one
    var initial = app.dataset.initialEntry;
    if (initial && initial !== "0") {
        load(initial);
    } else {
        var first = rows()[0];
        if (first) select(first.dataset.id);
    }
})();

// Disable upload buttons while a (possibly large) file is processed
document.addEventListener("submit", function (ev) {
    var busy = ev.target.getAttribute && ev.target.getAttribute("data-busy");
    if (busy && !ev.defaultPrevented) {
        var btn = ev.target.querySelector("button[type=submit]");
        if (btn) { btn.disabled = true; btn.textContent = busy; }
    }
});

// Suggest the release name and version from an uploaded file name such as
// "gnome-51.ui.id.2026-11-08.tar.gz" -> "gnome" + "51"
(function () {
    var file = document.getElementById("gl-file");
    if (!file) return;
    file.addEventListener("change", function () {
        var f = file.files && file.files[0];
        if (!f) return;
        var m = /^([A-Za-z0-9][A-Za-z0-9._]*?)-([A-Za-z0-9][A-Za-z0-9+~]*)(?:[._]|$)/.exec(f.name);
        if (!m) return;
        var name = document.getElementById("gl-name");
        var version = document.getElementById("gl-version");
        if (!version.value) version.value = m[2];
        if (!name.value || name.value !== m[1].toLowerCase() && !name.dataset.touched) name.value = m[1].toLowerCase();
    });
    document.getElementById("gl-name").addEventListener("input", function (ev) { ev.target.dataset.touched = "1"; });
})();

// Tabs inside dialogs: buttons with data-tab show the matching data-panel
(function () {
    function showTab(root, name) {
        root.querySelectorAll(".tabbtn").forEach(function (b) { b.classList.toggle("active", b.dataset.tab === name); });
        root.querySelectorAll(".tabpanel").forEach(function (p) { p.hidden = p.dataset.panel !== name; });
    }
    document.querySelectorAll("dialog .tabbar").forEach(function (bar) {
        var dlg = bar.closest("dialog");
        showTab(dlg, dlg.dataset.tabInitial || bar.querySelector(".tabbtn").dataset.tab);
    });
    document.addEventListener("click", function (ev) {
        var btn = ev.target.closest(".tabbtn, [data-tab-switch]");
        if (btn) {
            var dlg = btn.closest("dialog");
            if (dlg) showTab(dlg, btn.dataset.tab || btn.dataset.tabSwitch);
        }
        var opener = ev.target.closest("[data-tab-open]");
        if (opener) {
            var target = document.getElementById(opener.dataset.openDialog);
            if (target) showTab(target, opener.dataset.tabOpen);
        }
    });

    // Glossary source: switch between file upload and download link
    var form = document.getElementById("glossary-source-form");
    if (!form) return;
    function sync() {
        var mode = form.querySelector("input[name=source]:checked").value;
        form.querySelectorAll("[data-source]").forEach(function (el) { el.hidden = el.dataset.source !== mode; });
        document.getElementById("gl-file").required = mode === "file";
        document.getElementById("gl-url").required = mode === "url";
    }
    form.addEventListener("change", function (ev) { if (ev.target.name === "source") sync(); });
    sync();

    // Suggest name + version from a download link such as .../gnome-51/ui.tar.gz
    document.getElementById("gl-url").addEventListener("change", function (ev) {
        var m = /\/([a-z0-9][a-z0-9._]*?)-([0-9][A-Za-z0-9.+~]*)(?:\/|$)/i.exec(ev.target.value);
        var version = document.getElementById("gl-version");
        if (m && !version.value) {
            version.value = m[2];
            var name = document.getElementById("gl-name");
            if (!name.dataset.touched) name.value = m[1].toLowerCase();
        }
    });
})();

// Raw PO editor: Ctrl+S saves, cursor position, unsaved-change guard
(function () {
    var form = document.getElementById("raw-form");
    if (!form) return;
    var ta = document.getElementById("raw-text");
    var pos = document.getElementById("raw-pos");
    var dirty = false;
    function updatePos() {
        var before = ta.value.slice(0, ta.selectionStart).split("\n");
        pos.textContent = before.length + ":" + (before[before.length - 1].length + 1);
    }
    ta.addEventListener("input", function () { dirty = true; updatePos(); });
    ta.addEventListener("click", updatePos);
    ta.addEventListener("keyup", updatePos);
    ta.addEventListener("keydown", function (ev) {
        if (ev.key === "Tab" && !ev.ctrlKey && !ev.metaKey) {
            ev.preventDefault();
            ta.setRangeText("\t", ta.selectionStart, ta.selectionEnd, "end");
            dirty = true;
        }
    });
    document.addEventListener("keydown", function (ev) {
        if ((ev.ctrlKey || ev.metaKey) && (ev.key === "s" || ev.key === "S")) {
            ev.preventDefault();
            dirty = false;
            form.requestSubmit ? form.requestSubmit() : form.submit();
        }
    });
    form.addEventListener("submit", function () { dirty = false; });
    window.addEventListener("beforeunload", function (ev) {
        if (dirty) { ev.preventDefault(); ev.returnValue = form.dataset.msgUnsaved; }
    });
    // jump to the line reported in a parse error ("baris 12" / "line 12")
    var err = document.querySelector(".raw-error");
    var m = err && /(?:baris|line) (\d+)/.exec(err.textContent);
    if (m) {
        var lines = ta.value.split("\n"), offset = 0, n = parseInt(m[1], 10);
        for (var i = 0; i < n - 1 && i < lines.length; i++) offset += lines[i].length + 1;
        ta.focus();
        ta.setSelectionRange(offset, offset + (lines[n - 1] || "").length);
        ta.scrollTop = Math.max(0, (n - 5) * 20);
    }
    updatePos();
})();

// AI pre-translation dialog: refresh the cost estimate when opened or changed
(function () {
    var dlg = document.getElementById("ai-dialog");
    var box = document.getElementById("ai-estimate-box");
    if (!dlg || !box) return;
    var form = document.getElementById("ai-form");
    var seq = 0;
    function load() {
        var n = ++seq;
        var params = new URLSearchParams(new FormData(form));
        box.style.opacity = "0.5";
        fetch(box.dataset.url + "?" + params.toString(), { headers: { "HX-Request": "true" }, credentials: "same-origin" })
            .then(function (r) { return r.text(); })
            .then(function (html) {
                if (n !== seq) return; // a newer request is on its way
                box.innerHTML = html;
                box.style.opacity = "";
            })
            .catch(function () { box.style.opacity = ""; });
    }
    dlg.addEventListener("dialog-open", load);
    form.addEventListener("change", load);
})();

// History timeline: highlight the selected version
document.addEventListener("click", function (ev) {
    var item = ev.target.closest(".timeline [data-select]");
    if (!item) return;
    document.querySelectorAll(".timeline .selected").forEach(function (el) { el.classList.remove("selected"); });
    item.classList.add("selected");
});

// Discussions: lazy threads in the glossary list, mention chips, inline edit
(function () {
    function loadThread(box) {
        if (box.dataset.loaded) return;
        box.dataset.loaded = "1";
        fetch(box.dataset.threadUrl, { headers: { "HX-Request": "true" }, credentials: "same-origin" })
            .then(function (r) { return r.text(); })
            .then(function (html) {
                box.innerHTML = html;
                if (window.htmx) htmx.process(box);
            })
            .catch(function () { delete box.dataset.loaded; });
    }
        window.loadThreads = function (root) {
        if (!root) return;
        root.querySelectorAll(".gl-thread[data-thread-url]").forEach(loadThread);
    };
    document.addEventListener("toggle", function (ev) {
        var d = ev.target;
        if (d.tagName === "DETAILS" && d.open) {
            var box = d.querySelector(".gl-thread[data-thread-url]");
            if (box) loadThread(box);
        }
    }, true);

    document.addEventListener("click", function (ev) {
        var chip = ev.target.closest("[data-mention]");
        if (chip) {
            var ta = chip.closest("form").querySelector("textarea");
            var text = "@" + chip.dataset.mention + " ";
            var pos = ta.selectionStart || ta.value.length;
            var before = ta.value.slice(0, pos);
            if (before && !/\s$/.test(before)) text = " " + text;
            ta.setRangeText(text, pos, ta.selectionEnd || pos, "end");
            ta.focus();
            return;
        }
        var edit = ev.target.closest("[data-edit-comment]");
        if (edit) {
            var li = document.getElementById("comment-" + edit.dataset.editComment);
            li.querySelector(".comment-body").hidden = true;
            var f = li.querySelector(".comment-edit");
            f.hidden = false;
            f.querySelector("textarea").focus();
            return;
        }
        var cancel = ev.target.closest("[data-cancel-edit]");
        if (cancel) {
            var item = cancel.closest(".comment");
            item.querySelector(".comment-edit").hidden = true;
            item.querySelector(".comment-body").hidden = false;
        }
    });

    // Ctrl+Enter sends a comment
    document.addEventListener("keydown", function (ev) {
        if ((ev.ctrlKey || ev.metaKey) && ev.key === "Enter" && ev.target.closest(".comment-form, .comment-edit")) {
            ev.preventDefault();
            ev.stopPropagation();
            var f = ev.target.closest("form");
            f.requestSubmit ? f.requestSubmit() : htmx.trigger(f, "submit");
        }
    }, true);

    // highlight a linked comment
    if (location.hash.indexOf("#comment-") === 0) {
        var c = document.querySelector(location.hash);
        if (c) c.classList.add("comment-target");
    }
})();

// Suggest a project address from its name until the user edits it
(function () {
    document.querySelectorAll("input[data-slug-for]").forEach(function (slug) {
        var name = slug.dataset.slugFor && document.getElementById(slug.dataset.slugFor);
        if (!name) return;
        slug.addEventListener("input", function () { slug.dataset.touched = "1"; });
        name.addEventListener("input", function () {
            if (slug.dataset.touched) return;
            slug.value = name.value.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "")
                .replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 46).replace(/-+$/, "");
        });
    });
})();

// "Resend code" countdown: the button stays disabled for this address's own wait
(function () {
    function start(btn) {
        if (btn.dataset.counting) return;
        var left = parseInt(btn.dataset.countdown, 10) || 0;
        if (left <= 0) return;
        btn.dataset.counting = "1";
        btn.disabled = true;
        var tick = function () {
            if (!document.body.contains(btn)) return;
            if (left <= 0) {
                btn.disabled = false;
                btn.textContent = btn.dataset.label;
                delete btn.dataset.counting;
                return;
            }
            btn.textContent = btn.dataset.waitLabel.replace("%d", left);
            left--;
            setTimeout(tick, 1000);
        };
        tick();
    }
    function scan(root) {
        (root || document).querySelectorAll("[data-countdown]").forEach(start);
    }
    scan(document);
    document.body.addEventListener("htmx:afterSwap", function (ev) { scan(ev.detail.target.parentNode || document); });
})();

// Comment attachments: choose, paste or drag & drop images; preview what will be sent
(function () {
    var IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
    function input(form) { return form.querySelector('input[type=file][name=files]'); }
    function showError(form, msgs) {
        var box = form.querySelector(".att-error");
        if (!box) return;
        box.textContent = msgs.join(" ");
        box.hidden = !msgs.length;
    }
    function render(form) {
        var inp = input(form), list = form.querySelector(".att-pending");
        if (!inp || !list) return;
        list.querySelectorAll("img").forEach(function (img) { URL.revokeObjectURL(img.src); });
        list.innerHTML = "";
        Array.prototype.forEach.call(inp.files, function (f, i) {
            var li = document.createElement("li");
            var img = document.createElement("img");
            img.src = URL.createObjectURL(f);
            img.alt = f.name;
            li.appendChild(img);
            var name = document.createElement("span");
            name.className = "att-pending-name";
            name.textContent = f.name + " · " + Math.max(1, Math.round(f.size / 1024)) + " KB";
            li.appendChild(name);
            var rm = document.createElement("button");
            rm.type = "button";
            rm.className = "att-remove";
            rm.textContent = "×";
            rm.addEventListener("click", function () {
                var dt = new DataTransfer();
                Array.prototype.forEach.call(inp.files, function (g, j) { if (j !== i) dt.items.add(g); });
                inp.files = dt.files;
                showError(form, []);
                render(form);
            });
            li.appendChild(rm);
            list.appendChild(li);
        });
    }
    // add keeps only images within the size and count limits and explains what was skipped
    function add(form, files, replace) {
        var inp = input(form);
        if (!inp) return;
        var max = parseInt(inp.dataset.max, 10), maxBytes = parseInt(inp.dataset.maxBytes, 10);
        var dt = new DataTransfer(), errs = {};
        if (!replace) Array.prototype.forEach.call(inp.files, function (f) { dt.items.add(f); });
        files.forEach(function (f) {
            if (IMAGE_TYPES.indexOf(f.type) < 0) errs.type = inp.dataset.errType;
            else if (f.size > maxBytes) errs.size = inp.dataset.errSize;
            else if (dt.files.length >= max) errs.count = inp.dataset.errCount;
            else dt.items.add(f);
        });
        inp.files = dt.files;
        showError(form, Object.keys(errs).map(function (k) { return errs[k]; }));
        render(form);
        var ta = form.querySelector("textarea[name=body]");
        if (ta) ta.focus();
    }
    document.addEventListener("change", function (ev) {
        if (ev.target.matches('.comment-form input[name=files]')) {
            // the picker replaces the selection; re-check it against the limits
            add(ev.target.closest("form"), Array.prototype.slice.call(ev.target.files), true);
        }
    });
    // paste screenshots into the comment box
    document.addEventListener("paste", function (ev) {
        var form = ev.target.closest && ev.target.closest(".comment-form");
        if (!form || !ev.clipboardData) return;
        var files = [];
        Array.prototype.forEach.call(ev.clipboardData.items, function (it) {
            if (it.kind === "file") {
                var f = it.getAsFile();
                if (f) {
                    var ext = (f.type.split("/")[1] || "png").replace("jpeg", "jpg");
                    files.push(new File([f], f.name && f.name !== "image." + ext ? f.name : "tempel-" + Date.now() + "." + ext, { type: f.type }));
                }
            }
        });
        if (files.length) {
            ev.preventDefault();
            add(form, files);
        }
    });
    // drag & drop anywhere on a discussion thread that has a comment form
    function dropForm(ev) {
        var thread = ev.target.closest && ev.target.closest(".thread");
        return thread && thread.querySelector(".comment-form");
    }
    function hasFiles(ev) {
        return ev.dataTransfer && Array.prototype.indexOf.call(ev.dataTransfer.types || [], "Files") >= 0;
    }
    var dragDepth = 0;
    function clearDrop() {
        dragDepth = 0;
        document.querySelectorAll(".thread.drop-active").forEach(function (t) { t.classList.remove("drop-active"); });
    }
    document.addEventListener("dragenter", function (ev) {
        var form = dropForm(ev);
        if (!form || !hasFiles(ev)) return;
        dragDepth++;
        var thread = form.closest(".thread");
        thread.dataset.dropLabel = form.dataset.dropLabel || "";
        thread.classList.add("drop-active");
    });
    document.addEventListener("dragleave", function (ev) {
        if (!dropForm(ev) || !hasFiles(ev)) return;
        if (--dragDepth <= 0) clearDrop();
    });
    document.addEventListener("dragover", function (ev) {
        if (dropForm(ev) && hasFiles(ev)) {
            ev.preventDefault();
            ev.dataTransfer.dropEffect = "copy";
        }
    });
    document.addEventListener("drop", function (ev) {
        var form = dropForm(ev);
        clearDrop();
        if (!form || !ev.dataTransfer || !ev.dataTransfer.files.length) return;
        ev.preventDefault();
        add(form, Array.prototype.slice.call(ev.dataTransfer.files));
    });
    document.addEventListener("dragend", clearDrop);
    // a successful post replaces the thread; clear any leftover state
    document.body.addEventListener("htmx:afterSwap", clearDrop);
})();

// Image lightbox for comment attachments: fit to screen, click for original size
(function () {
    var dlg, img, link, caption, items = [], index = 0;
    function build(thread) {
        if (dlg) return;
        dlg = document.createElement("dialog");
        dlg.className = "lightbox";
        dlg.innerHTML = '<div class="lb-bar"><span class="lb-caption"></span><span class="lb-hint muted small"></span>' +
            '<a class="lb-open btn btn-small" download></a><button type="button" class="lb-close" aria-label="">×</button></div>' +
            '<button type="button" class="lb-nav lb-prev" aria-label="‹">‹</button>' +
            '<div class="lb-stage"><img alt=""></div>' +
            '<button type="button" class="lb-nav lb-next" aria-label="›">›</button>';
        document.body.appendChild(dlg);
        img = dlg.querySelector("img");
        link = dlg.querySelector(".lb-open");
        caption = dlg.querySelector(".lb-caption");
        link.textContent = thread.dataset.lbOpen || "↗";
        dlg.querySelector(".lb-hint").textContent = thread.dataset.lbZoom || "";
        dlg.querySelector(".lb-close").setAttribute("aria-label", thread.dataset.lbClose || "×");
        dlg.querySelector(".lb-close").addEventListener("click", function () { dlg.close(); });
        dlg.querySelector(".lb-prev").addEventListener("click", function () { show(index - 1); });
        dlg.querySelector(".lb-next").addEventListener("click", function () { show(index + 1); });
        img.addEventListener("click", function (ev) {
            ev.stopPropagation();
            dlg.classList.toggle("lb-actual");
        });
        // clicking the dark area around the image closes it
        dlg.addEventListener("click", function (ev) {
            if (ev.target === dlg || ev.target.classList.contains("lb-stage")) dlg.close();
        });
        dlg.addEventListener("keydown", function (ev) {
            if (ev.key === "ArrowLeft") show(index - 1);
            if (ev.key === "ArrowRight") show(index + 1);
        });
        dlg.addEventListener("close", function () { img.removeAttribute("src"); });
    }
    function show(i) {
        if (!items.length) return;
        index = (i + items.length) % items.length;
        var a = items[index];
        // items are attachment links (a[data-lightbox]) or images in a wiki page
        var src = a.tagName === "IMG" ? (a.currentSrc || a.src) : a.href;
        var thumb = a.tagName === "IMG" ? a : a.querySelector("img");
        dlg.classList.remove("lb-actual");
        img.src = src;
        img.alt = thumb ? thumb.alt : "";
        link.href = src;
        // save under the uploaded file name (same-origin, so the browser honours it)
        link.setAttribute("download", decodeURIComponent(src.split("?")[0].split("/").pop()) || "");
        caption.textContent = a.title || img.alt || decodeURIComponent(src.split("/").pop());
        dlg.classList.toggle("lb-single", items.length < 2);
    }
    document.addEventListener("click", function (ev) {
        if (!ev.target.closest || ev.ctrlKey || ev.metaKey || ev.shiftKey || ev.button !== 0) return;
        var a = ev.target.closest("a[data-lightbox]");
        var scope;
        if (a) {
            scope = a.closest(".comment") || document;
            items = Array.prototype.slice.call(scope.querySelectorAll("a[data-lightbox]"));
        } else if (ev.target.matches(".wiki-body img")) {
            // a linked image keeps its link
            if (ev.target.closest("a")) return;
            a = ev.target;
            scope = a.closest(".wiki-body");
            items = Array.prototype.slice.call(scope.querySelectorAll("img")).filter(function (i) { return !i.closest("a"); });
        } else {
            return;
        }
        ev.preventDefault();
        build(a.closest(".thread, [data-lb-open]") || document.body);
        show(items.indexOf(a));
        if (!dlg.open) dlg.showModal();
    });
})();

// Copy buttons (e.g. invite links)
document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest("[data-copy]");
    if (!btn) return;
    var text = btn.getAttribute("data-copy");
    var done = function () {
        var old = btn.textContent;
        btn.textContent = btn.getAttribute("data-copied") || "✓";
        setTimeout(function () { btn.textContent = old; }, 1500);
    };
    if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(done, function () {});
    } else {
        var inp = btn.parentNode.querySelector("input");
        if (inp) { inp.select(); try { document.execCommand("copy"); done(); } catch (e) {} }
    }
});

// Wiki editor: write/preview tabs, image uploads (picker, drag & drop, paste) and
// a warning before leaving with unsaved text
(function () {
    var form = document.getElementById("wiki-form");
    if (!form) return;
    var ta = document.getElementById("wiki-body");
    var write = form.querySelector('[data-wpanel="write"]');
    var preview = form.querySelector('[data-wpanel="preview"]');
    var errBox = write.querySelector(".att-error");
    var MAX_BYTES = 10 * 1024 * 1024;
    var dirty = false, uploads = 0;

    function showError(msg) { errBox.textContent = msg || ""; errBox.hidden = !msg; }

    // tabs
    form.querySelectorAll("[data-wtab]").forEach(function (tab) {
        tab.addEventListener("click", function () {
            var which = tab.dataset.wtab;
            form.querySelectorAll("[data-wtab]").forEach(function (t) {
                t.classList.toggle("active", t === tab);
                t.setAttribute("aria-selected", t === tab ? "true" : "false");
            });
            write.hidden = which !== "write";
            preview.hidden = which !== "preview";
            if (which === "preview") {
                preview.innerHTML = '<p class="muted">…</p>';
                var body = new URLSearchParams();
                body.set("body", ta.value);
                fetch(form.dataset.previewUrl, { method: "POST", body: body, credentials: "same-origin" })
                    .then(function (r) { return r.text(); })
                    .then(function (html) { preview.innerHTML = html; })
                    .catch(function () { preview.innerHTML = ""; });
            } else {
                ta.focus();
            }
        });
    });

    // uploads: each file is stored right away and its Markdown inserted at the cursor
    // (images as pictures, other files as a 📎 download link)
    function insertAtCursor(text) {
        var start = ta.selectionStart, end = ta.selectionEnd;
        var before = ta.value.slice(0, start);
        var pad = before && !/\n$/.test(before) ? "\n" : "";
        ta.setRangeText(pad + text + "\n", start, end, "end");
        dirty = true;
    }
    function upload(files) {
        var ok = [], errs = [];
        files.forEach(function (f) {
            if (f.size > MAX_BYTES) errs.push(ta.dataset.errSize);
            else ok.push(f);
        });
        showError(errs.filter(function (e, i) { return errs.indexOf(e) === i; }).join(" "));
        if (!ok.length) return;
        // a placeholder marks where the images will go while they upload
        var marker = "[" + ta.dataset.uploading + " " + ok.map(function (f) { return f.name; }).join(", ") + "…]()";
        insertAtCursor(marker);
        var fd = new FormData();
        ok.forEach(function (f) { fd.append("files", f, f.name); });
        uploads++;
        fetch(form.dataset.uploadUrl, { method: "POST", body: fd, credentials: "same-origin" })
            .then(function (r) { return r.json(); })
            .then(function (res) {
                var md = res.files ? res.files.map(function (f) { return f.markdown; }).join("\n") : "";
                ta.value = ta.value.replace(marker, md);
                if (res.error) showError(res.error);
            })
            .catch(function () { ta.value = ta.value.replace(marker, ""); showError(ta.dataset.errType); })
            .finally(function () { uploads--; });
    }
    document.getElementById("wiki-files").addEventListener("change", function (ev) {
        upload(Array.prototype.slice.call(ev.target.files));
        ev.target.value = "";
    });
    ta.addEventListener("paste", function (ev) {
        var files = [];
        Array.prototype.forEach.call((ev.clipboardData || {}).items || [], function (it) {
            if (it.kind === "file") {
                var f = it.getAsFile();
                if (f) {
                    var ext = (f.type.split("/")[1] || "png").replace("jpeg", "jpg");
                    files.push(new File([f], f.name && f.name !== "image." + ext ? f.name : "tempel-" + Date.now() + "." + ext, { type: f.type }));
                }
            }
        });
        if (files.length) { ev.preventDefault(); upload(files); }
    });
    var depth = 0;
    function hasFiles(ev) { return ev.dataTransfer && Array.prototype.indexOf.call(ev.dataTransfer.types || [], "Files") >= 0; }
    write.addEventListener("dragenter", function (ev) { if (hasFiles(ev)) { depth++; write.classList.add("drop-active"); } });
    write.addEventListener("dragleave", function (ev) { if (hasFiles(ev) && --depth <= 0) { depth = 0; write.classList.remove("drop-active"); } });
    write.addEventListener("dragover", function (ev) { if (hasFiles(ev)) { ev.preventDefault(); ev.dataTransfer.dropEffect = "copy"; } });
    write.addEventListener("drop", function (ev) {
        depth = 0;
        write.classList.remove("drop-active");
        if (!ev.dataTransfer || !ev.dataTransfer.files.length) return;
        ev.preventDefault();
        // drop where the pointer is when the browser can tell
        ta.focus();
        upload(Array.prototype.slice.call(ev.dataTransfer.files));
    });

    // unsaved changes
    form.addEventListener("input", function () { dirty = true; });
    form.addEventListener("submit", function (ev) {
        if (uploads > 0) { ev.preventDefault(); showError(ta.dataset.uploading + "…"); return; }
        dirty = false;
    });
    window.addEventListener("beforeunload", function (ev) {
        if (dirty) { ev.preventDefault(); ev.returnValue = ""; }
    });
})();

// Wiki sidebar: drag pages to reorder them or make them subpages
(function () {
    var tree = document.getElementById("wiki-tree");
    if (!tree) return;
    var maxDepth = parseInt(tree.dataset.maxDepth, 10) || 3;
    var dragged = null, target = null, mode = null;

    function itemOf(el) { return el && el.closest ? el.closest("#wiki-tree li[data-id]") : null; }
    function depth(li) { return parseInt(li.dataset.depth, 10) || 1; }
    // levels from li down to its deepest subpage (1 = no subpages)
    function height(li) {
        var h = 1;
        li.querySelectorAll("li[data-id]").forEach(function (c) { h = Math.max(h, depth(c) - depth(li) + 1); });
        return h;
    }
    function clear() {
        tree.querySelectorAll(".drop-before, .drop-after, .drop-inside, .drop-bad").forEach(function (li) {
            li.classList.remove("drop-before", "drop-after", "drop-inside", "drop-bad");
        });
    }
    function allowed(li, m) {
        if (!dragged || li === dragged || dragged.contains(li)) return false;
        var newDepth = m === "inside" ? depth(li) + 1 : depth(li);
        return newDepth + height(dragged) - 1 <= maxDepth;
    }

    tree.addEventListener("dragstart", function (ev) {
        dragged = itemOf(ev.target);
        if (!dragged) return;
        ev.dataTransfer.effectAllowed = "move";
        ev.dataTransfer.setData("text/plain", dragged.dataset.slug);
        dragged.classList.add("dragging");
    });
    tree.addEventListener("dragend", function () {
        if (dragged) dragged.classList.remove("dragging");
        dragged = target = mode = null;
        clear();
    });
    tree.addEventListener("dragover", function (ev) {
        var li = itemOf(ev.target);
        if (!dragged || !li) return;
        var a = li.querySelector(":scope > a");
        var r = a.getBoundingClientRect();
        var y = (ev.clientY - r.top) / r.height;
        var m = y < 0.3 ? "before" : y > 0.7 ? "after" : "inside";
        clear();
        target = li;
        mode = m;
        if (allowed(li, m)) {
            ev.preventDefault();
            ev.dataTransfer.dropEffect = "move";
            li.classList.add("drop-" + m);
        } else {
            li.classList.add("drop-bad");
            mode = null;
        }
    });
    tree.addEventListener("dragleave", function (ev) {
        if (!tree.contains(ev.relatedTarget)) clear();
    });
    tree.addEventListener("drop", function (ev) {
        ev.preventDefault();
        var li = dragged, t = target, m = mode;
        clear();
        if (!li || !t || !m) return;
        var body = new URLSearchParams();
        body.set("target_id", t.dataset.id);
        body.set("mode", m);
        fetch(tree.dataset.moveUrl + encodeURIComponent(li.dataset.slug) + "/move", { method: "POST", body: body, credentials: "same-origin" })
            .then(function (r) { return r.json(); })
            .then(function (res) {
                if (res.error) { window.showMessage(res.error); return; }
                location.reload();
            })
            .catch(function () { location.reload(); });
    });
})();

// Searchable dropdown (glossary module filter): a hidden input holds the value;
// choosing an option fires "change" on it so the surrounding form refreshes.
(function () {
    function close(combo) {
        if (!combo) return;
        combo.querySelector(".combo-pop").hidden = true;
        combo.querySelector(".combo-btn").setAttribute("aria-expanded", "false");
    }
    function open(combo) {
        document.querySelectorAll("[data-combo]").forEach(function (c) { if (c !== combo) close(c); });
        var pop = combo.querySelector(".combo-pop"), q = combo.querySelector(".combo-q");
        pop.hidden = false;
        combo.querySelector(".combo-btn").setAttribute("aria-expanded", "true");
        q.value = "";
        filter(combo);
        q.focus();
        var sel = combo.querySelector(".combo-list button.selected");
        if (sel) sel.scrollIntoView({ block: "nearest" });
    }
    function visible(combo) {
        return Array.prototype.filter.call(combo.querySelectorAll(".combo-list li"), function (li) { return !li.hidden; })
            .map(function (li) { return li.querySelector("button"); });
    }
    function filter(combo) {
        var q = combo.querySelector(".combo-q").value.trim().toLowerCase();
        var any = false;
        combo.querySelectorAll(".combo-list li").forEach(function (li) {
            var b = li.querySelector("button");
            var show = !q || b.dataset.value === "" || b.dataset.label.toLowerCase().indexOf(q) >= 0;
            li.hidden = !show;
            if (show && b.dataset.value !== "") any = true;
        });
        combo.querySelector(".combo-none").hidden = any || !q;
    }
    function choose(combo, b) {
        var input = combo.querySelector('input[type=hidden]');
        combo.querySelector(".combo-label").textContent = b.dataset.label;
        combo.querySelector(".combo-btn").classList.toggle("combo-set", b.dataset.value !== "");
        combo.querySelectorAll(".combo-list button").forEach(function (x) { x.classList.toggle("selected", x === b); });
        close(combo);
        combo.querySelector(".combo-btn").focus();
        if (input.value !== b.dataset.value) {
            input.value = b.dataset.value;
            input.dispatchEvent(new Event("change", { bubbles: true }));
        }
    }
    document.addEventListener("click", function (ev) {
        var combo = ev.target.closest && ev.target.closest("[data-combo]");
        document.querySelectorAll("[data-combo]").forEach(function (c) { if (c !== combo) close(c); });
        if (!combo) return;
        if (ev.target.closest(".combo-btn")) {
            combo.querySelector(".combo-pop").hidden ? open(combo) : close(combo);
        } else {
            var b = ev.target.closest(".combo-list button");
            if (b) choose(combo, b);
        }
    });
    // typing in the search box filters the list and must not submit the form
    ["input", "change", "keyup"].forEach(function (type) {
        document.addEventListener(type, function (ev) {
            if (!ev.target.matches || !ev.target.matches(".combo-q")) return;
            ev.stopPropagation();
            if (type === "input") filter(ev.target.closest("[data-combo]"));
        }, true);
    });
    document.addEventListener("keydown", function (ev) {
        var combo = ev.target.closest && ev.target.closest("[data-combo]");
        if (!combo || combo.querySelector(".combo-pop").hidden) return;
        var items = visible(combo), i = items.indexOf(document.activeElement);
        if (ev.key === "Escape") { ev.preventDefault(); close(combo); combo.querySelector(".combo-btn").focus(); }
        else if (ev.key === "ArrowDown") { ev.preventDefault(); (items[i + 1] || items[0]).focus(); }
        else if (ev.key === "ArrowUp") { ev.preventDefault(); if (i <= 0) combo.querySelector(".combo-q").focus(); else items[i - 1].focus(); }
        else if (ev.key === "Enter" && ev.target.matches(".combo-q")) {
            ev.preventDefault();
            // Enter picks the first match (skipping "all modules" while searching)
            var pick = items.filter(function (b) { return b.dataset.value !== "" || !ev.target.value.trim(); })[0];
            if (pick) choose(combo, pick);
        }
    });
})();

// Copy the text of another element (e.g. the wiki text after an edit conflict)
document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest("[data-copy-from]");
    if (!btn) return;
    var src = document.querySelector(btn.getAttribute("data-copy-from"));
    if (!src) return;
    var done = function () {
        var old = btn.textContent;
        btn.textContent = btn.getAttribute("data-copied") || "✓";
        setTimeout(function () { btn.textContent = old; }, 1500);
    };
    if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(src.value || src.textContent).then(done, function () {});
    } else {
        src.focus();
        src.select();
        try { document.execCommand("copy"); done(); } catch (e) {}
    }
});

// On narrow screens the project tabs scroll sideways; keep the active tab in view
(function () {
    var nav = document.querySelector(".tabs-nav");
    var active = nav && nav.querySelector("a.active");
    if (nav && active && nav.scrollWidth > nav.clientWidth) {
        nav.scrollLeft = active.offsetLeft - (nav.clientWidth - active.offsetWidth) / 2;
    }
})();

// Show <time data-local> timestamps in the reader's time zone (the server may run on UTC)
(function () {
    function pad(n) { return (n < 10 ? "0" : "") + n; }
    window.formatLocalTime = function (el) {
        var d = new Date(el.getAttribute("datetime"));
        if (isNaN(d)) return;
        el.textContent = pad(d.getDate()) + "-" + pad(d.getMonth() + 1) + "-" + d.getFullYear() + " " + pad(d.getHours()) + ":" + pad(d.getMinutes());
    };
    document.querySelectorAll("time[data-local][datetime]").forEach(window.formatLocalTime);
})();

// Checkbox dropdowns ([data-multi-filter], e.g. the Berkas proyek version filter): none ticked
// means all; "Tampilkan semua" clears (same as the editor's status filter). Changes bubble to the form.
(function () {
    function label(box) {
        var l = box.querySelector(".mf-label");
        var boxes = box.querySelectorAll("input[type=checkbox]");
        var on = Array.prototype.filter.call(boxes, function (cb) { return cb.checked; });
        if (!on.length) l.textContent = l.dataset.all;
        else if (on.length === 1) l.textContent = on[0].parentNode.querySelector(".mf-text").textContent;
        else l.textContent = l.dataset.many.replace("%d", on.length);
        box.classList.toggle("mf-active", on.length > 0);
    }
    function setAll(box, value) {
        var boxes = box.querySelectorAll("input[type=checkbox]");
        boxes.forEach(function (cb) { cb.checked = value; });
        label(box);
        if (boxes[0]) boxes[0].dispatchEvent(new Event("change", { bubbles: true }));
    }
    document.addEventListener("change", function (ev) {
        var box = ev.target.closest && ev.target.closest("[data-multi-filter]");
        if (box) label(box);
    });
    document.addEventListener("click", function (ev) {
        document.querySelectorAll("[data-multi-filter]").forEach(function (box) {
            if (ev.target.closest(".mf-none") && box.contains(ev.target)) { setAll(box, false); box.open = false; }
            else if (box.open && !box.contains(ev.target)) box.open = false;
        });
    });
    // dropdowns rendered with ticked boxes show their selection
    document.querySelectorAll("[data-multi-filter]").forEach(label);
    document.addEventListener("htmx:load", function (ev) {
        if (ev.detail && ev.detail.elt && ev.detail.elt.querySelectorAll) ev.detail.elt.querySelectorAll("[data-multi-filter]").forEach(label);
    });
    document.addEventListener("keydown", function (ev) {
        if (ev.key !== "Escape") return;
        document.querySelectorAll("[data-multi-filter][open]").forEach(function (box) { box.open = false; box.querySelector("summary").focus(); });
    });
})();

// Commit button: disabled while there is nothing to commit. The badge inside it is
// refreshed out-of-band after every save (and after a commit); follow it.
(function () {
    var btn = document.querySelector(".tb-commit");
    if (!btn) return;
    var title = btn.title;
    function sync() {
        var badge = document.getElementById("commit-badge");
        var has = !!(badge && badge.textContent.trim());
        btn.disabled = !has;
        btn.title = has ? title : (btn.dataset.titleNone || title);
    }
    new MutationObserver(sync).observe(btn, { childList: true, subtree: true, characterData: true });
    sync();
})();

// Notification settings: "no email at all" greys out the individual switches
document.addEventListener("change", function (ev) {
    if (!ev.target.matches || !ev.target.matches("[data-notify-mute]")) return;
    var kinds = ev.target.form.querySelector(".notify-kinds");
    if (kinds) kinds.toggleAttribute("data-muted", ev.target.checked);
});

// Progress chart: a floating card follows the pointer and shows the nearest point
(function () {
    function pad(n) { return (n < 10 ? "0" : "") + n; }
    // dates under the chart in the reader's time zone
    function localLabels(root) {
        (root || document).querySelectorAll(".gc-xlabels [data-t]").forEach(function (el) {
            var d = new Date(el.dataset.t);
            if (isNaN(d)) return;
            var s = pad(d.getDate()) + "-" + pad(d.getMonth() + 1);
            if (el.hasAttribute("data-with-time")) s += " " + pad(d.getHours()) + ":" + pad(d.getMinutes());
            el.textContent = s;
        });
    }
    localLabels();
    document.addEventListener("htmx:load", function (ev) { if (ev.detail && ev.detail.elt && ev.detail.elt.querySelectorAll) localLabels(ev.detail.elt); });
    function nearest(svg, clientX) {
        var rect = svg.getBoundingClientRect();
        var vb = svg.viewBox.baseVal;
        var x = (clientX - rect.left) / rect.width * vb.width;
        var best = null, dist = Infinity;
        svg.querySelectorAll(".gc-pt").forEach(function (g) {
            var d = Math.abs(parseFloat(g.dataset.x) - x);
            if (d < dist) { dist = d; best = g; }
        });
        return best;
    }
    function hide(chart) {
        var tip = chart.querySelector(".gc-tip"), guide = chart.querySelector(".gc-guide");
        if (tip) tip.hidden = true;
        if (guide) guide.setAttribute("hidden", "");
        chart.querySelectorAll(".gc-pt.on").forEach(function (g) { g.classList.remove("on"); });
    }
    function show(chart, svg, ev) {
        var g = nearest(svg, ev.clientX);
        var tip = chart.querySelector(".gc-tip"), guide = chart.querySelector(".gc-guide");
        if (!g || !tip) return;
        chart.querySelectorAll(".gc-pt.on").forEach(function (o) { if (o !== g) o.classList.remove("on"); });
        g.classList.add("on");
        guide.setAttribute("x1", g.dataset.x);
        guide.setAttribute("x2", g.dataset.x);
        guide.removeAttribute("hidden");
        var d = new Date(g.dataset.t);
        tip.textContent = "";
        var head = document.createElement("div");
        head.className = "gc-tip-date";
        head.textContent = pad(d.getDate()) + "-" + pad(d.getMonth() + 1) + "-" + d.getFullYear() + " " + pad(d.getHours()) + ":" + pad(d.getMinutes());
        tip.appendChild(head);
        ["translated", "fuzzy", "untranslated"].forEach(function (k) {
            var row = document.createElement("div");
            row.className = "gc-tip-row gc-" + k;
            var sw = document.createElement("span");
            sw.className = "gc-swatch";
            var label = document.createElement("span");
            label.textContent = tip.dataset["l" + k.charAt(0).toUpperCase() + k.slice(1)];
            var val = document.createElement("strong");
            val.textContent = g.dataset[k];
            row.appendChild(sw); row.appendChild(label); row.appendChild(val);
            tip.appendChild(row);
        });
        tip.hidden = false;
        // place it beside the point, inside the chart
        var plot = chart.querySelector(".gc-plot").getBoundingClientRect();
        var rect = svg.getBoundingClientRect();
        var px = rect.left - plot.left + parseFloat(g.dataset.x) / svg.viewBox.baseVal.width * rect.width;
        var left = px + 14;
        if (left + tip.offsetWidth > plot.width) left = px - 14 - tip.offsetWidth;
        tip.style.left = Math.max(0, left) + "px";
        tip.style.top = Math.max(0, ev.clientY - plot.top - tip.offsetHeight / 2) + "px";
    }
    document.addEventListener("mousemove", function (ev) {
        var svg = ev.target.closest && ev.target.closest(".gc-svg");
        document.querySelectorAll(".group-chart").forEach(function (chart) {
            if (svg && chart.contains(svg)) show(chart, svg, ev);
            else hide(chart);
        });
    });
    document.addEventListener("touchstart", function (ev) {
        var svg = ev.target.closest && ev.target.closest(".gc-svg");
        if (!svg) return;
        var t = ev.touches[0];
        show(svg.closest(".group-chart"), svg, { clientX: t.clientX, clientY: t.clientY });
    }, { passive: true });
})();

// Konteks popup in the editor: the module's shared note, saved without leaving the page.
// The form carries the version it was opened with; a save after someone else's is refused
// (409) and the conflict box offers to copy the user's text and load the latest version.
(function () {
    var form = document.getElementById("context-form");
    if (!form) return;
    var ta = form.querySelector("textarea[name=note]");
    var ver = form.querySelector("input[name=version]");
    var box = form.querySelector(".context-conflict");
    var latest = null;

    function pad(n) { return (n < 10 ? "0" : "") + n; }
    function renderEdits(edits) {
        var det = form.querySelector(".context-history");
        if (!det || !edits) return;
        var ul = det.querySelector(".context-history-list");
        ul.textContent = "";
        edits.forEach(function (e) {
            var li = document.createElement("li");
            if (e.username) {
                var b = document.createElement("strong");
                b.textContent = "@" + e.username;
                li.appendChild(b);
            } else {
                li.appendChild(document.createTextNode("—"));
            }
            var span = document.createElement("span");
            span.className = "muted";
            span.appendChild(document.createTextNode(" · "));
            var t = document.createElement("time");
            var d = new Date(e.at);
            t.setAttribute("datetime", e.at);
            t.title = e.ago || "";
            t.textContent = pad(d.getDate()) + "-" + pad(d.getMonth() + 1) + "-" + d.getFullYear() + " " + pad(d.getHours()) + ":" + pad(d.getMinutes());
            span.appendChild(t);
            li.appendChild(document.createTextNode(" "));
            li.appendChild(span);
            ul.appendChild(li);
        });
        det.querySelector(".context-history-n").textContent = edits.length;
        det.hidden = edits.length === 0;
    }

    if (box) {
        box.querySelector("[data-context-copy]").addEventListener("click", function () {
            var btn = this;
            var done = function () { btn.textContent = btn.dataset.copied; };
            if (navigator.clipboard) navigator.clipboard.writeText(ta.value).then(done, function () { ta.select(); });
            else { ta.select(); document.execCommand("copy"); done(); }
        });
        box.querySelector("[data-context-load]").addEventListener("click", function () {
            if (!latest) return;
            window.askConfirm(this.dataset.confirmMsg, { ok: this.textContent }).then(function (yes) {
                if (!yes) return;
                ta.value = latest.note || "";
                ver.value = latest.version || 0;
                box.hidden = true;
                latest = null;
                ta.focus();
            });
        });
    }

    form.addEventListener("submit", function (ev) {
        ev.preventDefault();
        if (!ta) return;
        var status = form.querySelector(".context-status");
        var btn = form.querySelector("button[type=submit]");
        var body = new URLSearchParams();
        body.set("note", ta.value);
        if (ver) body.set("version", ver.value);
        btn.disabled = true;
        fetch(form.dataset.url, { method: "POST", body: body, credentials: "same-origin" })
            .then(function (r) { return r.json(); })
            .then(function (res) {
                if (res.conflict && box) {
                    latest = { note: res.note, version: res.version };
                    box.querySelector(".context-conflict-help").textContent = res.help || "";
                    var cp = box.querySelector("[data-context-copy]");
                    if (!cp.dataset.label) cp.dataset.label = cp.textContent;
                    cp.textContent = cp.dataset.label;
                    box.hidden = false;
                    renderEdits(res.edits);
                    status.textContent = "";
                    box.scrollIntoView({ block: "nearest" });
                    return;
                }
                if (res.error) { status.textContent = res.error; status.classList.add("c-err"); return; }
                if (box) box.hidden = true;
                if (ver && res.version) ver.value = res.version;
                status.classList.remove("c-err");
                status.textContent = res.status;
                renderEdits(res.edits);
                var dot = document.getElementById("context-dot");
                if (dot) dot.hidden = !res.has;
                form.closest("dialog").close();
                if (window.toast) window.toast(res.status);
            })
            .catch(function () { status.classList.add("c-err"); })
            .finally(function () { btn.disabled = false; });
    });
})();

// Thesaurus: select a word or short phrase in the translation box to see synonyms;
// clicking one replaces the selection (keeping a leading capital)
(function () {
    var app = document.getElementById("editor-app");
    if (!app || !app.dataset.thesaurus) return;
    var cache = {}, pop = null, timer = null, current = null;
    var tags = {};
    (app.dataset.thTags || "").split(",").forEach(function (p) { var kv = p.split(":"); if (kv.length === 2) tags[kv[0]] = kv[1]; });

    function close() { if (pop) { pop.remove(); pop = null; } current = null; }

    // pixel position of a character offset inside a textarea (mirror element technique)
    function caretXY(ta, pos) {
        var cs = getComputedStyle(ta), div = document.createElement("div");
        ["boxSizing", "width", "borderTopWidth", "borderLeftWidth", "borderRightWidth", "paddingTop", "paddingLeft",
         "paddingRight", "fontFamily", "fontSize", "fontWeight", "fontStyle", "lineHeight", "letterSpacing",
         "wordSpacing", "textTransform", "textIndent", "tabSize"].forEach(function (k) { div.style[k] = cs[k]; });
        div.style.position = "absolute"; div.style.visibility = "hidden"; div.style.top = "0"; div.style.left = "-9999px";
        div.style.whiteSpace = "pre-wrap"; div.style.overflowWrap = "break-word"; div.style.height = "auto";
        div.textContent = ta.value.slice(0, pos);
        var span = document.createElement("span");
        span.textContent = ta.value.slice(pos) || ".";
        div.appendChild(span);
        document.body.appendChild(div);
        var lh = parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.4;
        var out = { x: span.offsetLeft - ta.scrollLeft, y: span.offsetTop - ta.scrollTop + lh };
        div.remove();
        return out;
    }

    function matchCase(word, sample) {
        if (sample === sample.toUpperCase() && sample !== sample.toLowerCase() && sample.length > 1) return word.toUpperCase();
        if (sample.charAt(0) !== sample.charAt(0).toLowerCase()) return word.charAt(0).toUpperCase() + word.slice(1);
        return word;
    }

    function replace(ta, start, end, text) {
        if (window.trBeforeChange) window.trBeforeChange();
        ta.focus();
        ta.setRangeText(text, start, end, "select");
        ta.dispatchEvent(new Event("input", { bubbles: true }));
        close();
    }

    function show(ta, start, end, res) {
        close();
        var sample = ta.value.slice(start, end);
        pop = document.createElement("div");
        pop.className = "th-pop";
        pop.setAttribute("role", "dialog");
        var head = document.createElement("div");
        head.className = "th-head";
        if (!res.found) {
            head.textContent = app.dataset.thNone;
            pop.appendChild(head);
            pop.classList.add("th-empty");
        } else {
            head.innerHTML = "";
            var w = document.createElement("strong"); w.textContent = res.asForm ? sample.trim() : res.word; head.appendChild(w);
            if (res.tag) { var t = document.createElement("span"); t.className = "th-tag"; t.textContent = tags[res.tag] || res.tag; head.appendChild(t); }
            if (res.asForm) { var f = document.createElement("span"); f.className = "th-form"; f.textContent = app.dataset.thAsForm.replace("%s", res.word); head.appendChild(f); }
            pop.appendChild(head);
            [["synonyms", app.dataset.thTitle], ["antonyms", app.dataset.thAntonyms]].forEach(function (kind) {
                var list = res[kind[0]] || [];
                if (!list.length) return;
                var label = document.createElement("div"); label.className = "th-label"; label.textContent = kind[1];
                pop.appendChild(label);
                var box = document.createElement("div"); box.className = "th-words";
                list.forEach(function (word) {
                    var b = document.createElement("button");
                    b.type = "button"; b.className = "th-word" + (kind[0] === "antonyms" ? " th-ant" : "");
                    b.textContent = word;
                    b.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
                    b.addEventListener("click", function () { replace(ta, start, end, matchCase(word, sample)); });
                    box.appendChild(b);
                });
                pop.appendChild(box);
            });
            var src = document.createElement("div"); src.className = "th-src"; src.textContent = app.dataset.thSource;
            pop.appendChild(src);
        }
        document.body.appendChild(pop);
        var r = ta.getBoundingClientRect(), xy = caretXY(ta, start);
        var top = r.top + Math.min(xy.y + 4, r.height), left = r.left + xy.x;
        var pw = pop.offsetWidth, ph = pop.offsetHeight;
        if (left + pw > window.innerWidth - 8) left = window.innerWidth - pw - 8;
        if (top + ph > window.innerHeight - 8) top = r.top + xy.y - ph - 28;
        pop.style.left = Math.max(8, left) + "px";
        pop.style.top = Math.max(8, top) + "px";
        current = { ta: ta, start: start, end: end };
        if (!res.found) setTimeout(function () { if (pop && pop.classList.contains("th-empty")) close(); }, 1800);
    }

    function check(ta) {
        if (ta.readOnly) { close(); return; }
        clearTimeout(timer);
        var start = ta.selectionStart, end = ta.selectionEnd;
        var text = ta.value.slice(start, end);
        var q = text.trim();
        if (!q || q.length > 60 || /\n/.test(q) || !/\p{L}/u.test(q)) { if (!current || current.ta === ta) close(); return; }
        if (current && current.ta === ta && current.start === start && current.end === end) return;
        // trim surrounding spaces from the replaced range too
        var lead = text.length - text.replace(/^\s+/, "").length, trail = text.length - text.replace(/\s+$/, "").length;
        start += lead; end -= trail;
        timer = setTimeout(function () {
            var key = q.toLowerCase();
            var done = function (res) { if (ta.selectionStart !== ta.selectionEnd) show(ta, start, end, res); };
            if (cache[key]) return done(cache[key]);
            fetch(app.dataset.thesaurus + "&q=" + encodeURIComponent(q), { credentials: "same-origin" })
                .then(function (r) { return r.json(); })
                .then(function (res) { cache[key] = res; done(res); })
                .catch(function () {});
        }, 180);
    }

    document.addEventListener("mouseup", function (ev) {
        if (ev.target.matches && ev.target.matches("#entry-form .tr-input")) setTimeout(function () { check(ev.target); }, 0);
        else if (pop && !pop.contains(ev.target)) close();
    });
    document.addEventListener("keyup", function (ev) {
        if (ev.target.matches && ev.target.matches("#entry-form .tr-input") && (ev.shiftKey || ev.key === "Shift")) check(ev.target);
    });
    document.addEventListener("keydown", function (ev) {
        if (ev.key === "Escape" && pop) { ev.stopPropagation(); close(); }
    }, true);
    document.addEventListener("input", function (ev) { if (pop && ev.target.matches && ev.target.matches(".tr-input")) close(); });
    // a new entry replaces the editor pane
    document.body.addEventListener("htmx:afterSwap", close);
    window.addEventListener("resize", close);
    document.addEventListener("scroll", function (ev) { if (pop && !pop.contains(ev.target)) close(); }, true);
})();

// "?" (outside text fields) opens the keyboard shortcut help in the editor
document.addEventListener("keydown", function (ev) {
    if (ev.key !== "?" || ev.ctrlKey || ev.metaKey || ev.altKey) return;
    var t = ev.target;
    if (t.isContentEditable || (t.matches && t.matches("input, textarea, select"))) return;
    var dlg = document.getElementById("help-dialog");
    if (dlg && !dlg.open && !document.querySelector("dialog[open]")) { ev.preventDefault(); dlg.showModal(); }
});
