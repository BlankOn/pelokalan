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

    // Remove toasts after a few seconds (also the one a page brings for its ?flash= message)
    var toastArea = document.getElementById("toast");
    if (toastArea) {
        var arm = function () {
            toastArea.querySelectorAll(".toast:not([data-timer])").forEach(function (t) {
                t.setAttribute("data-timer", "1");
                setTimeout(function () { t.remove(); }, 4500);
            });
        };
        new MutationObserver(arm).observe(toastArea, { childList: true });
        arm();
    }
    // PO syntax highlighting (raw editor, raw view): keywords, strings with escapes, placeholders and markup, and each
    // kind of comment; returns HTML
    window.poHighlight = (function () {
        function esc(t) { return t.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }
        function hlString(s) { // s is a quoted PO string, quotes included
            var out = "", re = /\\.|%(?:\d+\$)?[-+ 0#']*(?:\d+|\*)?(?:\.(?:\d+|\*))?(?:hh|h|ll|l|L|q|j|z|t)?[diouxXeEfFgGaAcspn%]|%\([A-Za-z0-9_]+\)[-+ 0#]*\d*(?:\.\d+)?[diouxXeEfFgGcrsa]|\{[A-Za-z0-9_.]*\}|<\/?[A-Za-z][\w:.-]*(?:\s[^<>"]*)?\/?>/g, last = 0, m;
            while ((m = re.exec(s))) {
                out += esc(s.slice(last, m.index));
                var cls = m[0][0] === "\\" ? "po-esc" : m[0][0] === "<" ? "po-tag" : "po-fmt";
                out += '<span class="' + cls + '">' + esc(m[0]) + "</span>";
                last = re.lastIndex;
            }
            return '<span class="po-str">' + out + esc(s.slice(last)) + "</span>";
        }
        function hlRest(rest) { // keyword line or continuation: highlight the quoted part
            var q = rest.indexOf('"');
            return q < 0 ? esc(rest) : esc(rest.slice(0, q)) + hlString(rest.slice(q));
        }
        var inId = false;
        function hlLine(line) {
            if (line[0] === "#") {
                var k = line.slice(0, 2), cls = "po-cm";
                if (k === "#:") cls = "po-ref";
                else if (k === "#.") cls = "po-dev";
                else if (k === "#|") cls = "po-prev";
                else if (k === "#~") cls = "po-obs";
                else if (k === "#,") {
                    return '<span class="po-flag">' + esc(line).replace(/\bfuzzy\b/, '<span class="po-fuzzy">fuzzy</span>') + "</span>";
                }
                return '<span class="' + cls + '">' + esc(line) + "</span>";
            }
            var m = /^(\s*)(msgctxt|msgid_plural|msgid|msgstr(?:\[\d+\])?)\b(.*)$/.exec(line);
            if (m) {
                inId = m[2] !== "msgstr" && m[2].indexOf("msgstr") !== 0;
                return esc(m[1]) + '<span class="po-kw">' + m[2] + "</span>" + (inId ? '<span class="po-id">' + hlRest(m[3]) + "</span>" : hlRest(m[3]));
            }
            if (/^\s*"/.test(line)) return inId ? '<span class="po-id">' + hlRest(line) + "</span>" : hlRest(line);
            inId = false;
            return esc(line);
        }
        return function (text) {
            inId = false;
            return text.split("\n").map(hlLine).join("\n");
        };
    })();
    // the flash message was shown as a toast: drop it from the address so a reload does not show it again
    try {
        var u = new URL(location.href);
        if (u.searchParams.has("flash")) {
            u.searchParams.delete("flash");
            history.replaceState(history.state, "", u.pathname + (u.search || "") + u.hash);
        }
    } catch (e) { /* old browser: keep the address */ }
    // a short message in the corner, on every page (the element <div id="toast"> would otherwise be window.toast)
    window.toast = function (message, isError) {
        var area = document.getElementById("toast");
        if (!area || !message) return;
        var t = document.createElement("div");
        t.className = "toast" + (isError ? " toast-error" : "");
        t.textContent = message;
        area.appendChild(t);
    };

    // Sinkronkan (project files): start the sync, spin until it is done, then report and refresh the list
    document.addEventListener("submit", function (ev) {
        var form = ev.target;
        if (!form.hasAttribute || !form.hasAttribute("data-sync")) return;
        ev.preventDefault();
        if (form.classList.contains("is-syncing")) return;
        var btn = form.querySelector("button");
        var label = form.querySelector(".sync-label");
        var sec = form.closest("[data-sticky-section]");
        var msg = (sec && sec.querySelector(".pf-sync-msg")) || document.querySelector(".pf-sync-msg");
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
                            // states and files may have changed: reload both lists with their current filters
                            if (window.htmx) {
                                ["pf-filter", "my-filter"].forEach(function (id) {
                                    var f = document.getElementById(id);
                                    if (f) htmx.trigger(f, "submit");
                                });
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
        if (window.spellInit) window.spellInit(ed);
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

    // AI translation of the current entry: the result goes into the translation box (unsaved).
    // With several alternatives (count selector beside the button, remembered per browser) they
    // are shown side by side above the box and the chosen one goes in.
    var aiBusy = false;
    function aiAltCount() {
        var sel = form() && form().querySelector("#ai-alt-count");
        return sel ? parseInt(sel.value, 10) || 1 : 1;
    }
    function restoreAltCount() {
        var sel = form() && form().querySelector("#ai-alt-count");
        if (!sel) return;
        try { var v = localStorage.getItem("aiAltCount"); if (v) sel.value = v; } catch (e) {}
    }
    document.addEventListener("change", function (ev) {
        if (ev.target.id === "ai-alt-count") { try { localStorage.setItem("aiAltCount", ev.target.value); } catch (e) {} }
    });
    document.body.addEventListener("htmx:load", restoreAltCount);
    restoreAltCount();

    // put one AI result (forms + flagged) into the translation box (Urungkan brings the old text back)
    function applyAIResult(f, res, notice) {
        var boxes = Array.prototype.slice.call(f.querySelectorAll(".tr-input"));
        pushUndo();
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
        var n = f.querySelector(".ed-notice");
        if (n) n.textContent = notice || "";
        focusTranslation();
    }

    function showAlternatives(f, res) {
        var box = f.querySelector("#ai-alts");
        if (!box) { applyAIResult(f, res.alternatives[0], res.notice); return; }
        box.textContent = "";
        var head = document.createElement("div");
        head.className = "ai-alts-head";
        var title = document.createElement("span");
        title.textContent = box.dataset.title;
        var close = document.createElement("button");
        close.type = "button";
        close.className = "linklike small";
        close.textContent = box.dataset.close;
        close.addEventListener("click", function () { box.hidden = true; });
        head.appendChild(title); head.appendChild(close);
        box.appendChild(head);
        var grid = document.createElement("div");
        grid.className = "ai-alts-grid";
        grid.style.gridTemplateColumns = "repeat(" + res.alternatives.length + ", minmax(0, 1fr))";
        // per plural form: the places where the alternatives differ, one colour per place in every card
        var marks = (res.alternatives[0].forms || []).map(function (f, k) {
            return altDiffs(res.alternatives.map(function (o) { return (o.forms && o.forms[k]) || ""; }));
        });
        res.alternatives.forEach(function (alt, i) {
            var card = document.createElement("div");
            card.className = "ai-alt" + (alt.flagged ? " ai-alt-flagged" : "");
            var num = document.createElement("div");
            num.className = "ai-alt-num";
            num.textContent = "#" + (i + 1) + (alt.flagged ? " · " + box.dataset.flagged : "");
            card.appendChild(num);
            alt.forms.forEach(function (text, k) {
                var t = document.createElement("div");
                t.className = "ai-alt-text";
                renderDiff(t, marks[k][i]);
                card.appendChild(t);
            });
            // the whole box is the choice: click (or Enter/Space) puts it in the translation box
            card.tabIndex = 0;
            card.setAttribute("role", "button");
            card.title = box.dataset.use;
            var choose = function () {
                applyAIResult(f, alt, res.notice);
                grid.querySelectorAll(".ai-alt").forEach(function (c) {
                    c.classList.toggle("ai-alt-chosen", c === card);
                    c.setAttribute("aria-pressed", c === card ? "true" : "false");
                });
            };
            card.addEventListener("click", choose);
            card.addEventListener("keydown", function (ev) {
                if (ev.key === "Enter" || ev.key === " ") { ev.preventDefault(); ev.stopPropagation(); choose(); }
            });
            grid.appendChild(card);
        });
        box.appendChild(grid);
        box.hidden = false;
    }

    function aiTranslateEntry() {
        var f = form(), btn = f && f.querySelector("#btn-ai-entry");
        if (!btn || readOnly || aiBusy || editorLocked()) return;
        var body = new URLSearchParams();
        var boxes = Array.prototype.slice.call(f.querySelectorAll(".tr-input"));
        boxes.forEach(function (ta) { body.set("msgstr" + ta.dataset.panel, ta.value); });
        body.set("n", aiAltCount());
        var alts = f.querySelector("#ai-alts");
        if (alts) alts.hidden = true;
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
                if (res.alternatives && res.alternatives.length > 1) showAlternatives(f, res);
                else applyAIResult(f, res, res.notice);
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

    // wordOps aligns two texts word by word (whitespace kept as its own tokens): a list of
    // {op: "eq"|"del"|"ins", a: index in a's tokens, b: index in b's tokens}
    function wordOps(x, y) {
        var n = x.length, m = y.length, L = [], i, j;
        for (i = 0; i <= n; i++) { L.push(new Array(m + 1).fill(0)); }
        for (i = n - 1; i >= 0; i--) for (j = m - 1; j >= 0; j--)
            L[i][j] = x[i] === y[j] ? L[i + 1][j + 1] + 1 : Math.max(L[i + 1][j], L[i][j + 1]);
        var ops = [];
        i = 0; j = 0;
        while (i < n && j < m) {
            if (x[i] === y[j]) ops.push({ op: "eq", a: i++, b: j++ });
            else if (L[i + 1][j] >= L[i][j + 1]) ops.push({ op: "del", a: i++, b: j });
            else ops.push({ op: "ins", a: i, b: j++ });
        }
        while (i < n) ops.push({ op: "del", a: i++, b: j });
        while (j < m) ops.push({ op: "ins", a: i, b: j++ });
        return ops;
    }
    // altDiffs marks the places where alternatives differ, with one colour per place in every card: each
    // alternative is aligned with #1, and a run of changed words (spaces between changed words included)
    // is keyed by where it starts in #1, so "mengekspos" in #1 and "memperlihatkan" in #2 share a colour.
    // Returns per text the parts for renderDiff: [tag, text, colour index].
    function altDiffs(texts) {
        var toks = texts.map(function (t) { return t.split(/(\s+)/); });
        var ref = toks[0], refKey = ref.map(function () { return null; });
        var keyed = toks.map(function (tk) { return tk.map(function () { return null; }); });
        for (var t = 1; t < toks.length; t++) {
            var ops = wordOps(ref, toks[t]), key = null;
            for (var o = 0; o < ops.length; o++) {
                var op = ops[o];
                if (op.op === "eq") {
                    // a space between two changes stays in the change; anything else ends it
                    var next = ops[o + 1];
                    if (key !== null && !ref[op.a].trim() && next && next.op !== "eq") continue;
                    key = null;
                    continue;
                }
                if (key === null) key = op.a; // where the change starts in #1
                if (op.op === "del") { if (refKey[op.a] === null) refKey[op.a] = key; }
                else keyed[t][op.b] = key;
            }
        }
        keyed[0] = refKey;
        // colours in reading order of #1
        var keys = [];
        keyed.forEach(function (ks) { ks.forEach(function (k) { if (k !== null && keys.indexOf(k) < 0) keys.push(k); }); });
        keys.sort(function (a, b) { return a - b; });
        return toks.map(function (tk, t) {
            return tk.map(function (w, i) {
                var k = keyed[t][i];
                return k === null ? ["", w] : ["mark", w, keys.indexOf(k)];
            });
        });
    }
    function renderDiff(el, parts) {
        el.innerHTML = "";
        parts.forEach(function (p) {
            var node = p[0] && p[1].trim() ? document.createElement(p[0]) : document.createTextNode(p[1]);
            if (p[0] && p[1].trim()) {
                node.textContent = p[1];
                // a colour per place (golden-angle hues, so neighbours differ clearly)
                if (p[2] !== undefined) node.style.setProperty("--h", Math.round((48 + p[2] * 137.508) % 360));
            }
            el.appendChild(node);
        });
    }
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
        var btn = ev.target.closest && ev.target.closest(".gl-insert[data-insert]");
        if (!btn || readOnly) return;
        var ta = activePanel();
        if (!ta || ta.readOnly) return;
        pushUndo();
        ta.setRangeText(btn.getAttribute("data-insert"), ta.selectionStart, ta.selectionEnd, "end");
        ta.focus();
        clearFuzzyOnEdit();
        setDirty(true);
    });

    // Translator credits: put the owner's name (or this year) into the credits text
    document.addEventListener("click", function (ev) {
        var btn = ev.target.closest && ev.target.closest("#entry-form .credit-fix[data-credit-fix]");
        if (!btn || readOnly) return;
        var ta = form().querySelector(".tr-input");
        if (!ta || ta.readOnly) return;
        pushUndo();
        ta.value = btn.getAttribute("data-credit-fix");
        ta.focus();
        clearFuzzyOnEdit();
        setDirty(true);
        // the identity is in the text now: drop this warning (and the box when it was the only one)
        var line = btn.parentNode, box = line.closest(".ed-alert");
        line.remove();
        if (box && !box.children.length) box.remove();
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
            if (src) ta.value = src.dataset.text != null ? src.dataset.text : src.textContent;
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
        if (row && app.classList.contains("review-mode") && String(window.getSelection()).trim()) return; // selecting text
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
    // Validasi ticks the statuses the user's editor settings keep on (data-statuses) and open review comments. When
    // nothing matches, the filter goes back to what it was (every string stays in view) and a toast says all is well.
    var validating = null;
    if (validate) validate.addEventListener("click", function () {
        var st = (validate.dataset.statuses || "issues,untranslated,fuzzy").split(",").filter(Boolean);
        if (st.indexOf("review") < 0) st.push("review");
        // the review editor also shows the strings whose review comments were resolved (the green rows)
        if (validate.dataset.resolved && st.indexOf("resolved") < 0) st.push("resolved");
        validating = filterBoxes().filter(function (cb) { return cb.checked; }).map(function (cb) { return cb.value; });
        setFilter(st);
    });
    document.body.addEventListener("htmx:afterSettle", function (ev) {
        if (!validating || !ev.detail || !ev.detail.target || ev.detail.target.id !== "entry-list") return;
        var before = validating;
        validating = null;
        var list = document.getElementById("entry-list");
        if (list && !list.querySelector(".entry-row")) {
            setFilter(before);
            if (window.toast) window.toast(validate.dataset.allClear);
        }
    });

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
            // Note: Ctrl/Cmd+digit is deliberately NOT bound — it is the browser's
            // tab-switch shortcut (Cmd+1..9 on macOS, Ctrl+1..8 on Windows/Linux).
            // Insert a translation suggestion by clicking it in the sidebar instead.
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

    // Sidebar: drag the divider to resize it (dragging it almost shut hides it); the handle in
    // the middle hides and shows it. Width and visibility are remembered per browser.
    var sideSplit = document.getElementById("side-splitter");
    var sideToggle = document.getElementById("side-toggle");
    var workspace = sideSplit && sideSplit.parentNode;
    function setSideWidth(px) {
        px = Math.round(Math.min(Math.max(220, px), workspace.clientWidth * 0.6));
        document.documentElement.style.setProperty("--sidebar-width", px + "px");
        return px;
    }
    // tablets (iPad mini portrait and landscape) and phones: the sidebar floats over the table and starts closed
    var floating = window.matchMedia("(max-width: 1180px)"); // tablets and phones
    // the review editor always shows the sidebar on wide screens: review comments are written there
    var reviewMode = !!(workspace && workspace.closest(".review-mode"));
    function setSideHidden(hidden, keep) {
        if (reviewMode && hidden && !floating.matches) return;
        workspace.classList.toggle("sidebar-hidden", hidden);
        var label = hidden ? sideToggle.dataset.show : sideToggle.dataset.hide;
        sideToggle.title = label;
        sideToggle.setAttribute("aria-label", label);
        if (keep === false || floating.matches) return; // a floating panel's state is not the wide layout's choice
        try { localStorage.setItem("sidebarHidden", hidden ? "1" : ""); } catch (e) { /* storage unavailable */ }
    }
    if (sideSplit) {
        try {
            var sw = parseInt(localStorage.getItem("sidebarWidth"), 10);
            if (sw) setSideWidth(sw);
            if (localStorage.getItem("sidebarHidden") === "1" && !reviewMode) setSideHidden(true);
        } catch (e) { /* storage unavailable */ }
        if (floating.matches) setSideHidden(true, false);
        // turning the tablet: closed when it starts floating, the remembered state when it docks again
        floating.addEventListener && floating.addEventListener("change", function (m) {
            var stored = false;
            try { stored = localStorage.getItem("sidebarHidden") === "1"; } catch (e) { /* storage unavailable */ }
            setSideHidden(m.matches ? true : stored && !reviewMode, false);
        });
        sideToggle.addEventListener("click", function (ev) {
            ev.stopPropagation();
            setSideHidden(!workspace.classList.contains("sidebar-hidden"));
        });
        sideToggle.addEventListener("pointerdown", function (ev) { ev.stopPropagation(); });
        sideSplit.addEventListener("pointerdown", function (ev) {
            if (ev.button !== 0 || workspace.classList.contains("sidebar-hidden") || floating.matches) return;
            ev.preventDefault();
            try { sideSplit.setPointerCapture(ev.pointerId); } catch (e) { /* not capturable */ }
            sideSplit.classList.add("dragging");
            var right = workspace.getBoundingClientRect().right, width = null;
            function move(e) {
                var w = right - e.clientX;
                sideSplit.classList.toggle("will-hide", w < 140);
                width = w < 140 ? null : setSideWidth(w);
            }
            function up(e) {
                sideSplit.classList.remove("dragging", "will-hide");
                document.removeEventListener("pointermove", move);
                document.removeEventListener("pointerup", up);
                document.removeEventListener("pointercancel", up);
                if (right - e.clientX < 140) { setSideHidden(true); return; }
                try { if (width) localStorage.setItem("sidebarWidth", width); } catch (err) { /* storage unavailable */ }
            }
            document.addEventListener("pointermove", move);
            document.addEventListener("pointerup", up);
            document.addEventListener("pointercancel", up);
        });
        sideSplit.addEventListener("keydown", function (ev) {
            if (ev.key !== "ArrowLeft" && ev.key !== "ArrowRight") return;
            ev.preventDefault();
            ev.stopPropagation();
            if (workspace.classList.contains("sidebar-hidden")) { if (ev.key === "ArrowLeft") setSideHidden(false); return; }
            var cur = document.getElementById("sidebar").offsetWidth;
            var w = setSideWidth(cur + (ev.key === "ArrowLeft" ? 24 : -24));
            try { localStorage.setItem("sidebarWidth", w); } catch (e) { /* storage unavailable */ }
        });
    }

        // open the entry named in the URL (?entry=…), otherwise the first one
    var initial = app.dataset.initialEntry;
    if (initial && initial !== "0") {
        load(initial);
    } else {
        var first = rows()[0];
        if (first) select(first.dataset.id);
    }
})();

// htmx requests that take longer than a second cover the page with a half transparent layer and
// a spinner, so nothing is clicked while the answer is on its way. Background polling (hx-trigger
// "every …") and elements inside [data-no-busy] never show it. The layer is a manual popover, so it
// also covers open dialogs; it gives up after 30 s so a hung request cannot lock the page for good.
(function () {
    var pending = new Set(), timer = null, failsafe = null, layer = null;
    function quiet(elt) {
        if (!elt || !elt.closest) return false;
        var trig = elt.getAttribute("hx-trigger") || "";
        return /(^|,)\s*every\s/.test(trig) || !!elt.closest("[data-no-busy]");
    }
    function build() {
        if (layer) return layer;
        layer = document.createElement("div");
        layer.className = "busy-overlay";
        layer.setAttribute("role", "status");
        layer.setAttribute("aria-live", "polite");
        layer.innerHTML = '<span class="busy-spinner" aria-hidden="true"></span><span class="sr-only"></span>';
        layer.querySelector(".sr-only").textContent = document.body.dataset.loading || "Loading…";
        if (layer.showPopover) layer.setAttribute("popover", "manual");
        document.body.appendChild(layer);
        return layer;
    }
    function show() {
        timer = null;
        if (!pending.size) return;
        var l = build();
        if (l.showPopover) {
            // shown again so it lands above a dialog opened since
            try { if (l.matches(":popover-open")) l.hidePopover(); l.showPopover(); } catch (e) { }
        }
        l.classList.add("on");
        failsafe = setTimeout(hide, 30000);
    }
    function hide() {
        clearTimeout(timer); timer = null;
        clearTimeout(failsafe); failsafe = null;
        if (!layer) return;
        layer.classList.remove("on");
        if (layer.hidePopover) { try { layer.hidePopover(); } catch (e) { } }
    }
    document.addEventListener("htmx:beforeSend", function (ev) {
        var xhr = ev.detail && ev.detail.xhr;
        if (!xhr || quiet(ev.detail.elt)) return;
        pending.add(xhr);
        if (!timer && !(layer && layer.classList.contains("on"))) timer = setTimeout(show, 1000);
    });
    function done(ev) {
        var xhr = ev.detail && ev.detail.xhr;
        if (!xhr || !pending.delete(xhr)) return;
        if (!pending.size) hide();
    }
    ["htmx:afterRequest", "htmx:sendError", "htmx:timeout", "htmx:xhr:abort"].forEach(function (name) {
        document.addEventListener(name, done);
    });
    window.addEventListener("pageshow", function () { pending.clear(); hide(); });
})();

// AI settings: the model dropdown fills the hidden model field; "Lainnya…" shows it for any other name
document.addEventListener("change", function (ev) {
    var pick = ev.target.closest && ev.target.closest("[data-model-pick]");
    if (!pick) return;
    var input = pick.parentNode.querySelector("input[name=model]");
    if (!input) return;
    input.hidden = pick.value !== "";
    if (pick.value) input.value = pick.value;
    else { input.value = ""; input.focus(); }
    // known models have built-in prices; only another model needs them typed in
    var form = pick.closest("form");
    if (form) form.querySelectorAll("[data-custom-price]").forEach(function (el) { el.hidden = pick.value !== ""; });
    var price = form && form.querySelector("[data-model-price]");
    if (price) price.textContent = pick.value ? (pick.selectedOptions[0].dataset.price || "") : "";
});

// Indonesian spelling check in the translation box (project members, own files; the browser's
// own check is off). The server says which words are unknown (POST /catalogs/{cid}/spell); a layer
// behind the transparent textarea repeats the text with those words underlined. Clicking an
// underlined word offers suggestions and "Tambahkan ke kamus proyek".
(function () {
    var app = document.getElementById("editor-app");
    if (!app || !app.dataset.spell) return;
    var active = true, unknown = {}, timer = 0, seq = 0, pop = null;
    var wordRe = /[\p{L}][\p{L}'’-]*[\p{L}]|[\p{L}]/gu;

    function esc(s) { return s.replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
    function source() {
        return Array.prototype.map.call(document.querySelectorAll("#editor .src-text[data-text]"), function (el) { return el.dataset.text; }).join("\n");
    }
    function boxes() { return Array.prototype.slice.call(document.querySelectorAll("#entry-form .tr-input")); }

    function layerFor(ta) {
        var l = ta.previousElementSibling;
        if (l && l.classList.contains("spell-layer")) return l;
        if (getComputedStyle(ta.parentNode).position === "static") ta.parentNode.style.position = "relative";
        l = document.createElement("div");
        l.className = "spell-layer";
        l.setAttribute("aria-hidden", "true");
        ta.parentNode.insertBefore(l, ta);
        ta.classList.add("spell-on");
        ta.addEventListener("scroll", function () { l.scrollTop = ta.scrollTop; });
        if (window.ResizeObserver) {
            new ResizeObserver(function () { place(ta, l); }).observe(ta);
            // the box can move without changing size (AI alternatives or a warning appear above it): its container
            // then changes size, so the layer follows instead of staying behind as a white box
            new ResizeObserver(function () { place(ta, l); }).observe(ta.parentNode);
        }
        return l;
    }
    // the layer copies the textarea's box and font so the text wraps at the same places
    function place(ta, l) {
        if (ta.hidden) { l.hidden = true; return; }
        l.hidden = false;
        var cs = getComputedStyle(ta);
        ["fontFamily", "fontSize", "fontWeight", "lineHeight", "letterSpacing", "paddingTop", "paddingRight", "paddingBottom",
            "paddingLeft", "borderTopWidth", "borderRightWidth", "borderBottomWidth", "borderLeftWidth", "borderRadius", "textIndent"].forEach(function (k) { l.style[k] = cs[k]; });
        l.style.top = ta.offsetTop + "px";
        l.style.left = ta.offsetLeft + "px";
        l.style.width = ta.offsetWidth + "px";
        l.style.height = ta.offsetHeight + "px";
        l.style.backgroundColor = cs.backgroundColor === "rgba(0, 0, 0, 0)" ? "" : cs.backgroundColor;
        l.scrollTop = ta.scrollTop;
    }
    function draw(ta) {
        var l = layerFor(ta), text = ta.value, out = "", last = 0, m;
        wordRe.lastIndex = 0;
        while ((m = wordRe.exec(text))) {
            if (!unknown[m[0].toLowerCase()]) continue;
            out += esc(text.slice(last, m.index)) + '<span class="typo" data-w="' + esc(m[0]) + '">' + esc(m[0]) + "</span>";
            last = m.index + m[0].length;
        }
        l.innerHTML = out + esc(text.slice(last)) + "\n";
        place(ta, l);
    }
    function drawAll() { boxes().forEach(draw); }

    function check() {
        if (!active) return;
        var list = boxes();
        if (!list.length) return;
        var mine = ++seq;
        fetch(app.dataset.spell, {
            method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ text: list.map(function (ta) { return ta.value; }).join("\n"), source: source() })
        }).then(function (r) { return r.ok ? r.json() : null; }).then(function (res) {
            if (!res || mine !== seq) return;
            if (res.active === false) { active = false; return; }
            unknown = {};
            (res.unknown || []).forEach(function (w) { unknown[w] = true; });
            drawAll();
        }).catch(function () { });
    }
    function later() { clearTimeout(timer); timer = setTimeout(check, 350); }

    window.spellInit = function () { close(); unknown = {}; drawAll(); check(); };
    document.addEventListener("input", function (ev) {
        if (!ev.target.matches || !ev.target.matches("#entry-form .tr-input")) return;
        draw(ev.target); // marks stay on the words until the next answer
        later();
    });
    document.addEventListener("click", function (ev) {
        if (ev.target.closest && ev.target.closest(".ptab")) setTimeout(drawAll, 0); // plural tabs
    });
    window.addEventListener("resize", function () { boxes().forEach(function (ta) { place(ta, layerFor(ta)); }); });

    // a click inside an underlined word opens its suggestions
    function close() { if (pop) { pop.remove(); pop = null; } }
    function wordAt(ta, pos) {
        var m;
        wordRe.lastIndex = 0;
        while ((m = wordRe.exec(ta.value))) {
            if (m.index <= pos && pos <= m.index + m[0].length) return { start: m.index, end: m.index + m[0].length, text: m[0] };
            if (m.index > pos) break;
        }
        return null;
    }
    function matchCase(sugg, orig) {
        if (orig === orig.toUpperCase() && orig.length > 1) return sugg.toUpperCase();
        if (orig[0] === orig[0].toUpperCase()) return sugg.charAt(0).toUpperCase() + sugg.slice(1);
        return sugg;
    }
    function replace(ta, w, text) {
        if (window.trBeforeChange) window.trBeforeChange();
        ta.value = ta.value.slice(0, w.start) + text + ta.value.slice(w.end);
        ta.setSelectionRange(w.start + text.length, w.start + text.length);
        ta.dispatchEvent(new Event("input", { bubbles: true }));
        ta.focus();
    }
    function open(ta, w) {
        close();
        var layer = layerFor(ta), mark = null;
        layer.querySelectorAll(".typo").forEach(function (s) { if (!mark && s.dataset.w === w.text) mark = s; });
        pop = document.createElement("div");
        pop.className = "th-pop spell-pop";
        pop.setAttribute("role", "dialog");
        pop.innerHTML = '<div class="th-head"><strong>' + esc(w.text) + '</strong><span class="th-tag">' + esc(app.dataset.spTitle) + '</span></div><div class="spell-list muted small">…</div>' +
            '<button type="button" class="linklike small spell-add">+ ' + esc(app.dataset.spAdd) + '</button>';
        document.body.appendChild(pop);
        var r = (mark || ta).getBoundingClientRect();
        pop.style.left = Math.max(8, Math.min(r.left, window.innerWidth - pop.offsetWidth - 8)) + "px";
        pop.style.top = (r.bottom + 6 + pop.offsetHeight > window.innerHeight ? r.top - pop.offsetHeight - 6 : r.bottom + 6) + "px";
        var current = pop;
        fetch(app.dataset.spellSuggest + "?w=" + encodeURIComponent(w.text), { credentials: "same-origin" })
            .then(function (r) { return r.json(); }).then(function (res) {
                if (pop !== current) return;
                var list = pop.querySelector(".spell-list");
                list.textContent = "";
                list.classList.remove("muted", "small");
                if (!res.suggestions || !res.suggestions.length) { list.textContent = app.dataset.spNone; list.classList.add("muted", "small"); return; }
                res.suggestions.forEach(function (s) {
                    var b = document.createElement("button");
                    b.type = "button";
                    b.className = "th-word";
                    b.textContent = matchCase(s, w.text);
                    b.addEventListener("click", function () { close(); replace(ta, w, b.textContent); });
                    list.appendChild(b);
                });
            }).catch(function () { });
        pop.querySelector(".spell-add").addEventListener("click", function () {
            fetch(app.dataset.spellDict, {
                method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ word: w.text })
            }).then(function (r) { return r.json(); }).then(function (res) {
                close();
                if (res.error) { window.showMessage(res.error); return; }
                delete unknown[(res.word || w.text).toLowerCase()];
                drawAll();
            }).catch(function () { });
        });
    }
    document.addEventListener("click", function (ev) {
        if (pop && !pop.contains(ev.target)) close();
        var ta = ev.target;
        if (!ta.matches || !ta.matches("#entry-form .tr-input") || ta.readOnly || ta.selectionStart !== ta.selectionEnd) return;
        var w = wordAt(ta, ta.selectionStart);
        if (w && unknown[w.text.toLowerCase()]) open(ta, w);
    });
    document.addEventListener("keydown", function (ev) { if (ev.key === "Escape") close(); });
    document.addEventListener("input", function (ev) { if (pop && ev.target.matches && ev.target.matches(".tr-input")) close(); });
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
    var file = document.getElementById("mg-file");
    if (!file) return;
    file.addEventListener("change", function () {
        var f = file.files && file.files[0];
        if (!f) return;
        var m = /^([A-Za-z0-9][A-Za-z0-9._]*?)-([A-Za-z0-9][A-Za-z0-9+~]*)(?:[._]|$)/.exec(f.name);
        if (!m) return;
        var name = document.getElementById("mg-name");
        var version = document.getElementById("mg-version");
        if (!version.value) version.value = m[2];
        if (!name.value || name.value !== m[1].toLowerCase() && !name.dataset.touched) name.value = m[1].toLowerCase();
    });
    document.getElementById("mg-name").addEventListener("input", function (ev) { ev.target.dataset.touched = "1"; });
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

    // Module group source: switch between file upload and sync link
    var form = document.getElementById("group-source-form");
    if (!form) return;
    function sync() {
        var mode = form.querySelector("input[name=source]:checked").value;
        form.querySelectorAll("[data-source]").forEach(function (el) { el.hidden = el.dataset.source !== mode; });
        document.getElementById("mg-file").required = mode === "file";
        document.getElementById("mg-url").required = mode === "url";
        var repo = document.getElementById("mg-git-repo"), branch = document.getElementById("mg-git-branch");
        if (repo) repo.required = mode === "git";
        if (branch) branch.required = mode === "git";
        var team = document.getElementById("mg-tp-team");
        if (team) team.required = mode === "tp";
        var lang = document.getElementById("mg-lang-field");
        if (lang) lang.hidden = mode === "tp"; // the team is the language
    }
    form.addEventListener("change", function (ev) { if (ev.target.name === "source") sync(); });
    sync();

    // GitHub repository: the name comes from the repository, the version from the branch
    // (https://github.com/python/python-docs-id/tree/3.14 → python-docs-id, 3.14)
    var gitRepo = document.getElementById("mg-git-repo"), gitBranch = document.getElementById("mg-git-branch");
    function suggestGit() {
        var m = /github\.com\/[^\/\s]+\/([^\/\s#?]+?)(?:\.git)?(?:\/tree\/([^\s#?]+))?\/?$/i.exec(gitRepo.value.trim());
        if (m && m[2] && !gitBranch.value) gitBranch.value = m[2];
        var name = document.getElementById("mg-name"), version = document.getElementById("mg-version");
        if (m && !name.dataset.touched) name.value = m[1].toLowerCase();
        if (gitBranch.value && !version.value) version.value = gitBranch.value.replace(/[^A-Za-z0-9._+~]/g, "-");
    }
    if (gitRepo && gitBranch) {
        gitRepo.addEventListener("change", suggestGit);
        gitBranch.addEventListener("change", suggestGit);
    }

    // Suggest name + version from a download link such as .../gnome-51/ui.tar.gz
    document.getElementById("mg-url").addEventListener("change", function (ev) {
        var m = /\/([a-z0-9][a-z0-9._]*?)-([0-9][A-Za-z0-9.+~]*)(?:\/|$)/i.exec(ev.target.value);
        var version = document.getElementById("mg-version");
        if (m && !version.value) {
            version.value = m[2];
            var name = document.getElementById("mg-name");
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
            ta.dispatchEvent(new Event("input")); // marks it dirty and refreshes the highlighting
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
        if (isDirty()) { ev.preventDefault(); ev.returnValue = form.dataset.msgUnsaved; }
    });

    // Leaving with changes (back button, links, browser Back) opens a popup with the line diff:
    // stay, discard, or save first and then go on
    var original = ta.value;
    var leave = document.getElementById("raw-leave-dialog");
    function isDirty() { return dirty && ta.value !== original; }
    function lineDiff(a, b) {
        var x = a.split("\n"), y = b.split("\n");
        var pre = 0;
        while (pre < x.length && pre < y.length && x[pre] === y[pre]) pre++;
        var suf = 0;
        while (suf < x.length - pre && suf < y.length - pre && x[x.length - 1 - suf] === y[y.length - 1 - suf]) suf++;
        var xm = x.slice(pre, x.length - suf), ym = y.slice(pre, y.length - suf), ops = [];
        if (xm.length * ym.length <= 4000000) {
            var n = xm.length, m = ym.length, L = new Array(n + 1);
            for (var i = 0; i <= n; i++) L[i] = new Uint32Array(m + 1);
            for (i = n - 1; i >= 0; i--) for (var j = m - 1; j >= 0; j--)
                L[i][j] = xm[i] === ym[j] ? L[i + 1][j + 1] + 1 : Math.max(L[i + 1][j], L[i][j + 1]);
            i = 0; j = 0;
            while (i < n && j < m) {
                if (xm[i] === ym[j]) { ops.push([" ", xm[i], pre + i + 1]); i++; j++; }
                else if (L[i + 1][j] >= L[i][j + 1]) ops.push(["-", xm[i], pre + i + 1]), i++;
                else ops.push(["+", ym[j], pre + j + 1]), j++;
            }
            while (i < n) ops.push(["-", xm[i], pre + i + 1]), i++;
            while (j < m) ops.push(["+", ym[j], pre + j + 1]), j++;
        } else { // too big to align: everything in between counts as replaced
            xm.forEach(function (l, k) { ops.push(["-", l, pre + k + 1]); });
            ym.forEach(function (l, k) { ops.push(["+", l, pre + k + 1]); });
        }
        // context: up to 2 unchanged lines around each change
        var before = x.slice(Math.max(0, pre - 2), pre).map(function (l, k) { return [" ", l, Math.max(0, pre - 2) + k + 1]; });
        var after = x.slice(x.length - suf, Math.min(x.length, x.length - suf + 2)).map(function (l, k) { return [" ", l, 0]; });
        return before.concat(ops, after);
    }
    function showDiff() {
        var ops = lineDiff(original, ta.value), pre = leave.querySelector(".raw-diff");
        var keep = ops.map(function (op, k) {
            if (op[0] !== " ") return true;
            for (var d = -2; d <= 2; d++) { var o = ops[k + d]; if (o && o[0] !== " ") return true; }
            return false;
        });
        var added = 0, removed = 0, shown = 0, hidden = 0, gap = false;
        pre.textContent = "";
        ops.forEach(function (op, k) {
            if (op[0] === "+") added++;
            if (op[0] === "-") removed++;
            if (!keep[k]) { gap = true; return; }
            if (shown >= 300) { if (op[0] !== " ") hidden++; return; }
            if (gap && shown) { var g = document.createElement("span"); g.className = "rd-gap"; g.textContent = "⋯\n"; pre.appendChild(g); }
            gap = false;
            var s = document.createElement("span");
            s.className = op[0] === "+" ? "rd-add" : op[0] === "-" ? "rd-del" : "rd-ctx";
            s.textContent = op[0] + " " + op[1] + "\n";
            pre.appendChild(s);
            shown++;
        });
        if (hidden) {
            var more = document.createElement("span");
            more.className = "rd-gap";
            more.textContent = leave.dataset.more.replace("999999", hidden);
            pre.appendChild(more);
        }
        leave.querySelector(".raw-diff-stats").textContent = leave.dataset.stats.replace("111111", added).replace("222222", removed);
    }
    var pendingHref = null;
    function askLeave(href) {
        if (!leave) return false;
        pendingHref = href;
        showDiff();
        leave.showModal();
        leave.querySelector("[data-raw-stay]").focus();
        return true;
    }
    if (leave) {
        leave.querySelector("[data-raw-stay]").addEventListener("click", function () { pendingHref = null; leave.close(); });
        leave.querySelector("[data-raw-discard]").addEventListener("click", function () {
            dirty = false;
            leave.close();
            if (pendingHref === "back") history.go(-2); else location.href = pendingHref;
        });
        leave.querySelector("[data-raw-save]").addEventListener("click", function () {
            form.querySelector("input[name=next]").value = pendingHref === "back" ? "" : pendingHref.replace(location.origin, "");
            dirty = false;
            leave.close();
            form.requestSubmit ? form.requestSubmit() : form.submit();
        });
        document.addEventListener("click", function (ev) {
            var a = ev.target.closest && ev.target.closest("a[href]");
            if (!a || !isDirty() || ev.defaultPrevented || ev.ctrlKey || ev.metaKey || ev.shiftKey || a.target === "_blank") return;
            if (/\/download/.test(a.getAttribute("href"))) return; // downloads do not leave the page
            ev.preventDefault();
            askLeave(a.href);
        });
        // browser Back: an extra history entry catches it while there are changes
        var guarded = false;
        ta.addEventListener("input", function () {
            if (!guarded) { history.pushState({ rawGuard: true }, ""); guarded = true; }
        });
        window.addEventListener("popstate", function () {
            if (!guarded) return;
            if (isDirty()) { history.pushState({ rawGuard: true }, ""); askLeave("back"); }
            else { guarded = false; history.back(); }
        });
    }
    // Syntax highlighting: keywords, strings with escapes and placeholders, and each kind of comment
    var hl = document.getElementById("raw-hl");
    function esc(t) { return t.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;"); }
    var hlFrame = 0;
    function highlight() {
        hlFrame = 0;
        // the trailing newline keeps the last line's height when the text ends with a line break
        hl.innerHTML = window.poHighlight(ta.value) + "\n";
        syncScroll();
    }
    function syncScroll() { hl.style.transform = "translate(" + -ta.scrollLeft + "px," + -ta.scrollTop + "px)"; }
    function copyMetrics() {
        var cs = getComputedStyle(ta), pre = hl.parentNode;
        ["fontSize", "lineHeight", "paddingTop", "paddingRight", "paddingBottom", "paddingLeft", "letterSpacing"].forEach(function (k) { pre.style[k] = cs[k]; });
    }
    if (hl && ta.value.length < 3000000) {
        copyMetrics();
        highlight();
        hl.parentNode.parentNode.classList.add("hl-on");
        ta.addEventListener("input", function () { if (!hlFrame) hlFrame = requestAnimationFrame(highlight); });
        ta.addEventListener("scroll", syncScroll);
        window.addEventListener("resize", function () { copyMetrics(); syncScroll(); });
    }

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

// Commit dialog: load the pending diff for review when the dialog opens
(function () {
    var dlg = document.getElementById("commit-dialog");
    var box = document.getElementById("commit-diff");
    if (!dlg || !box) return;
    var seq = 0;
    function load() {
        var n = ++seq;
        box.style.opacity = "0.5";
        fetch(box.dataset.url, { headers: { "HX-Request": "true" }, credentials: "same-origin" })
            .then(function (r) { return r.text(); })
            .then(function (html) {
                if (n !== seq) return; // a newer request is on its way
                box.innerHTML = html;
                box.style.opacity = "";
            })
            .catch(function () { box.style.opacity = ""; });
    }
    dlg.addEventListener("dialog-open", load);
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

// Copy buttons (e.g. invite links); data-copy-here copies this page's address (without #…)
document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest("[data-copy]");
    if (!btn) return;
    var text = btn.hasAttribute("data-copy-here") ? location.origin + location.pathname + location.search : btn.getAttribute("data-copy");
    var done = function () {
        var label = btn.querySelector("span") || btn; // keep an icon in place
        var old = label.textContent;
        label.textContent = btn.getAttribute("data-copied") || "✓";
        setTimeout(function () { label.textContent = old; }, 1500);
    };
    var fallback = function () { // no clipboard API (plain http): copy through a hidden field
        var inp = btn.parentNode.querySelector("input");
        var tmp = null;
        if (!inp) {
            tmp = inp = document.createElement("textarea");
            inp.value = text; inp.setAttribute("readonly", ""); inp.style.cssText = "position:fixed;opacity:0;top:0;left:0";
            document.body.appendChild(inp);
        }
        inp.select();
        try { document.execCommand("copy"); done(); } catch (e) { /* nothing more to try */ }
        if (tmp) tmp.remove();
    };
    if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(done, fallback);
    } else {
        fallback();
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
        (tip.dataset.rows || "translated fuzzy untranslated").split(" ").forEach(function (k) {
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
        document.querySelectorAll(".group-chart, .words-chart").forEach(function (chart) {
            if (svg && chart.contains(svg)) show(chart, svg, ev);
            else hide(chart);
        });
    });
    document.addEventListener("touchstart", function (ev) {
        var svg = ev.target.closest && ev.target.closest(".gc-svg");
        if (!svg) return;
        var t = ev.touches[0];
        show(svg.closest(".group-chart, .words-chart"), svg, { clientX: t.clientX, clientY: t.clientY });
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
    // the editor, the review/view layouts and the project file viewer carry data-thesaurus for members
    var app = document.querySelector("[data-thesaurus]");
    if (!app) return;
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

    // show the popup for sample under anchor (a screen rectangle); pick(word) runs when a word is clicked, else
    // (read-only tables) clicking a word copies it
    function show(sample, anchor, res, pick, top) {
        close();
        pop = document.createElement("div");
        if (top) pop.appendChild(top);
        pop.className = "th-pop";
        pop.setAttribute("role", "dialog");
        var head = document.createElement("div");
        head.className = "th-head";
        if (!res) {
            // no thesaurus lookup (a long passage): only the comment button
        } else if (!res.found) {
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
                    b.addEventListener("click", function () {
                        var w = matchCase(word, sample);
                        if (pick) { pick(w); return; }
                        if (navigator.clipboard && window.isSecureContext) navigator.clipboard.writeText(w).then(function () {
                            if (window.toast) window.toast((app.dataset.thCopied || "%s").replace("%s", w));
                        }, function () {});
                        close();
                    });
                    box.appendChild(b);
                });
                pop.appendChild(box);
            });
            var src = document.createElement("div"); src.className = "th-src"; src.textContent = app.dataset.thSource;
            pop.appendChild(src);
        }
        document.body.appendChild(pop);
        var top = anchor.bottom + 4, left = anchor.left;
        var pw = pop.offsetWidth, ph = pop.offsetHeight;
        if (left + pw > window.innerWidth - 8) left = window.innerWidth - pw - 8;
        if (top + ph > window.innerHeight - 8) top = anchor.top - ph - 6;
        pop.style.left = Math.max(8, left) + "px";
        pop.style.top = Math.max(8, top) + "px";
        if (res && !res.found && !top) setTimeout(function () { if (pop && pop.classList.contains("th-empty")) close(); }, 1800);
    }

    function lookup(q, done) {
        var key = q.toLowerCase();
        if (cache[key]) return done(cache[key]);
        fetch(app.dataset.thesaurus + "&q=" + encodeURIComponent(q), { credentials: "same-origin" })
            .then(function (r) { return r.json(); })
            .then(function (res) { cache[key] = res; done(res); })
            .catch(function () {});
    }
    function usable(q) { return q && q.length <= 60 && !/\n/.test(q) && /\p{L}/u.test(q); }

    function check(ta) {
        if (ta.readOnly) { close(); return; }
        clearTimeout(timer);
        var start = ta.selectionStart, end = ta.selectionEnd;
        var text = ta.value.slice(start, end);
        var q = text.trim();
        if (!usable(q)) { if (!current || current.ta === ta) close(); return; }
        if (current && current.ta === ta && current.start === start && current.end === end) return;
        // trim surrounding spaces from the replaced range too
        var lead = text.length - text.replace(/^\s+/, "").length, trail = text.length - text.replace(/\s+$/, "").length;
        start += lead; end -= trail;
        timer = setTimeout(function () {
            lookup(q, function (res) {
                if (ta.selectionStart === ta.selectionEnd) return;
                var r = ta.getBoundingClientRect(), xy = caretXY(ta, start);
                var y = r.top + Math.min(xy.y, r.height);
                show(ta.value.slice(start, end), { left: r.left + xy.x, top: y - 24, bottom: y }, res, function (w) { replace(ta, start, end, w); });
                current = { ta: ta, start: start, end: end };
            });
        }, 180);
    }

    // read-only tables (review and view layouts, project file viewer): a word selected in a translation shows its
    // synonyms; clicking one copies it
    function checkTable() {
        var sel = window.getSelection();
        if (!sel || sel.isCollapsed || !sel.rangeCount) return false;
        var node = sel.anchorNode && (sel.anchorNode.nodeType === 1 ? sel.anchorNode : sel.anchorNode.parentElement);
        var cell = node && node.closest && node.closest(".review-mode .entry-row .cell-tr");
        if (!cell || !cell.contains(sel.focusNode)) return false;
        var q = String(sel).trim();
        var canComment = app.dataset.anchorComment === "1" && q && q.length <= 300;
        if (!usable(q) && !canComment) { close(); return true; }
        var range = sel.getRangeAt(0), rect = range.getBoundingClientRect();
        var info = null;
        if (canComment) { // where the passage starts in the translation (first form, as the row shows it)
            var txt = cell.querySelector(".txt") || cell;
            var pre = document.createRange();
            pre.selectNodeContents(txt);
            pre.setEnd(range.startContainer, range.startOffset);
            var raw = String(sel), lead = raw.length - raw.replace(/^\s+/, "").length;
            info = { row: cell.closest(".entry-row"), text: q, start: pre.toString().length + lead, form: 0 };
        }
        function commentButton() {
            if (!info) return null;
            var b = document.createElement("button");
            b.type = "button";
            b.className = "btn btn-small th-comment";
            b.textContent = "💬 " + (app.dataset.anchorLabel || "Comment");
            b.addEventListener("mousedown", function (ev) { ev.preventDefault(); });
            b.addEventListener("click", function () { close(); window.startAnchorComment(info); });
            return b;
        }
        clearTimeout(timer);
        timer = setTimeout(function () {
            if (!usable(q)) { show(q, rect, null, null, commentButton()); current = { table: true }; return; }
            lookup(q, function (res) {
                if (String(window.getSelection()).trim() !== q) return;
                show(q, rect, res, null, commentButton());
                current = { table: true };
            });
        }, 180);
        return true;
    }

    document.addEventListener("mouseup", function (ev) {
        if (ev.target.matches && ev.target.matches("#entry-form .tr-input")) setTimeout(function () { check(ev.target); }, 0);
        else if (pop && pop.contains(ev.target)) return;
        else setTimeout(function () { if (!checkTable() && pop) close(); }, 0);
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

// Editor settings: "check all" ticks every rule of the dialog
document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest("[data-check-all]");
    if (!btn) return;
    var f = btn.closest("form");
    if (f) f.querySelectorAll("input[type=checkbox]").forEach(function (cb) { cb.checked = true; });
});

// History: "Ubah pesan" turns the version title into an input (saved with htmx)
document.addEventListener("click", function (ev) {
    var edit = ev.target.closest && ev.target.closest("[data-cm-edit]");
    var cancel = ev.target.closest && ev.target.closest("[data-cm-cancel]");
    var head = (edit || cancel) && (edit || cancel).closest(".diff-head");
    if (!head) return;
    var form = head.querySelector(".cm-edit"), title = head.querySelector(".cm-title");
    if (!form || !title) return;
    var actions = head.querySelector(".row-actions");
    form.hidden = !!cancel;
    title.hidden = !cancel;
    if (actions) actions.hidden = !cancel; // the field gets the whole row while editing
    if (edit) { var inp = form.querySelector("input"); inp.focus(); inp.select(); }
    else form.reset();
});
document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape") return;
    var form = ev.target.closest && ev.target.closest(".cm-edit");
    if (!form) return;
    ev.preventDefault();
    ev.stopPropagation();
    var c = form.querySelector("[data-cm-cancel]");
    if (c) c.click();
}, true);

// "Perbarui berkas…" in a file's menu: one popup for every file, filled from the button
// (download again from the saved link, or pick a file)
document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest("[data-replace]");
    var dlg = document.getElementById("replace-dialog");
    if (!btn || !dlg) return;
    ev.preventDefault();
    var form = dlg.querySelector("form");
    form.reset();
    form.action = btn.dataset.replace;
    dlg.querySelector(".replace-name").textContent = btn.dataset.replaceName || "";
    var url = btn.dataset.replaceUrl || "";
    dlg.querySelector("#r-url").value = url;
    dlg.querySelector(".replace-saved").hidden = !url;
    dlg.querySelector("#r-page").value = btn.dataset.replacePage || "";
    var menu = btn.closest("details");
    if (menu) menu.open = false;
    dlg.showModal();
    (url ? dlg.querySelector("button[type=submit]") : dlg.querySelector("#r-url")).focus();
});

// Review editor: table (the review's strings) or diff (the lines that will be committed)
document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest("[data-review-view]");
    var app = btn && btn.closest(".editor-app");
    if (!btn || !app) return;
    var view = btn.dataset.reviewView; // table, diff or raw
    app.classList.toggle("show-diff", view === "diff");
    app.classList.toggle("show-raw", view === "raw");
    var pane = document.getElementById("review-diff");
    if (pane) pane.hidden = view !== "diff";
    var raw = document.getElementById("raw-view");
    if (raw) {
        raw.hidden = view !== "raw";
        // the file's PO text, read once and highlighted like the raw editor (plain text for huge files)
        if (view === "raw" && !raw.dataset.loaded) {
            raw.dataset.loaded = "1";
            var pre = raw.querySelector("pre");
            pre.textContent = document.body.dataset.loading || "…";
            fetch(btn.dataset.rawSrc, { credentials: "same-origin" })
                .then(function (r) { if (!r.ok) throw new Error(r.status); return r.text(); })
                .then(function (text) {
                    if (text.length < 3000000) pre.innerHTML = window.poHighlight(text);
                    else pre.textContent = text;
                })
                .catch(function () { raw.dataset.loaded = ""; pre.textContent = "—"; });
        }
    }
    app.querySelectorAll("[data-review-view]").forEach(function (b) { b.classList.toggle("active", b === btn); });
});

// A thread's open review comments (data-review-open) mark the selected entry's row with a red dot, so
// resolving the last one (or a reviewer's new comment) shows in the list at once
document.addEventListener("htmx:load", function (ev) {
    var th = ev.target && ev.target.matches && ev.target.matches(".thread") ? ev.target : null;
    var row = document.querySelector(".entry-row.selected");
    if (!th || !row || !th.closest("#sidebar, #editor")) return;
    var n = parseInt(th.dataset.reviewOpen || "0", 10);
    // the count beside the sidebar handle follows this entry's change
    var before = parseInt(row.dataset.reviewOpen || "0", 10);
    row.dataset.reviewOpen = String(n);
    var badge = document.getElementById("side-review-badge");
    if (badge && n !== before) {
        var total = Math.max(0, parseInt(badge.dataset.n || "0", 10) + n - before);
        badge.dataset.n = String(total);
        badge.textContent = String(total);
    }
    var icon = row.querySelector(".cell-icon"), dot = icon && icon.querySelector(".mark-review");
    if (n > 0 && icon && !dot) {
        dot = document.createElement("span");
        dot.className = "mark-review";
        icon.insertBefore(dot, icon.firstChild);
    } else if (n === 0 && dot) {
        dot.remove();
    }
    // the last open one was resolved: the row and its passages turn green in the review editor
    if (n === 0 && before > 0) {
        row.classList.add("rv-resolved");
        row.querySelectorAll("mark.rv-anchor").forEach(function (m) { m.classList.add("rv-done"); });
    } else if (n > 0) {
        row.classList.remove("rv-resolved");
    }
});

// @mentions: typing "@" in a comment box offers the project's members (the thread's data-members) in a small popup.
// Arrow keys move, Enter or Tab picks, Esc (or simply typing on) ignores it, so a plain "@" stays possible.
(function () {
    var pop = null, state = null;
    function close() {
        if (pop) pop.remove();
        pop = null;
        state = null;
    }
    function membersOf(ta) {
        var host = ta.closest && ta.closest("[data-members]");
        if (!host) return null;
        try {
            var me = host.dataset.me || "";
            return JSON.parse(host.dataset.members).filter(function (n) { return n !== me; });
        } catch (e) { return null; }
    }
    // the "@word" right before the caret, when the @ starts a word
    function queryAt(ta) {
        if (ta.selectionStart !== ta.selectionEnd) return null;
        var before = ta.value.slice(0, ta.selectionStart);
        var m = /(^|[\s(\[{"'>])@([a-z0-9._-]{0,29})$/i.exec(before);
        return m ? { start: ta.selectionStart - m[2].length - 1, text: m[2].toLowerCase() } : null;
    }
    function choose(name) {
        if (!state) return;
        var ta = state.ta, start = state.start;
        close();
        ta.focus();
        ta.setRangeText("@" + name + " ", start, ta.selectionStart, "end");
        ta.dispatchEvent(new Event("input", { bubbles: true }));
    }
    function highlight(i) {
        var items = pop.querySelectorAll("button");
        state.active = (i + items.length) % items.length;
        items.forEach(function (b, j) { b.classList.toggle("active", j === state.active); });
        items[state.active].scrollIntoView({ block: "nearest" });
    }
    function show(ta, q, names) {
        if (!pop) {
            pop = document.createElement("div");
            pop.className = "mention-pop";
            pop.setAttribute("role", "listbox");
            document.body.appendChild(pop);
        }
        pop.textContent = "";
        names.forEach(function (n) {
            var b = document.createElement("button");
            b.type = "button";
            b.setAttribute("role", "option");
            var at = n.indexOf(q.text);
            b.appendChild(document.createTextNode("@" + n.slice(0, at)));
            var strong = document.createElement("b");
            strong.textContent = n.slice(at, at + q.text.length);
            b.appendChild(strong);
            b.appendChild(document.createTextNode(n.slice(at + q.text.length)));
            // mousedown keeps the focus in the comment box
            b.addEventListener("mousedown", function (ev) { ev.preventDefault(); choose(n); });
            pop.appendChild(b);
        });
        state = { ta: ta, start: q.start, active: 0 };
        var r = ta.getBoundingClientRect();
        var top = r.bottom + 4;
        pop.style.left = Math.max(8, Math.min(r.left, window.innerWidth - 290)) + "px";
        pop.style.top = top + "px";
        var h = pop.offsetHeight;
        if (top + h > window.innerHeight - 8) pop.style.top = Math.max(8, r.top - h - 4) + "px"; // no room below: above
        highlight(0);
    }
    document.addEventListener("input", function (ev) {
        var ta = ev.target;
        if (!ta.matches || !ta.matches("textarea")) return;
        var names = membersOf(ta), q = names && queryAt(ta);
        if (!q) { if (state && state.ta === ta) close(); return; }
        // names starting with what was typed first, then names containing it
        var starts = names.filter(function (n) { return n.indexOf(q.text) === 0; });
        var inside = names.filter(function (n) { return n.indexOf(q.text) > 0; });
        var list = starts.concat(inside).slice(0, 8);
        if (!list.length) { close(); return; }
        show(ta, q, list);
    });
    document.addEventListener("keydown", function (ev) {
        if (!state || ev.target !== state.ta || ev.ctrlKey || ev.metaKey || ev.altKey) return;
        if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
            ev.preventDefault(); ev.stopPropagation();
            highlight(state.active + (ev.key === "ArrowDown" ? 1 : -1));
        } else if (ev.key === "Enter" || ev.key === "Tab") {
            ev.preventDefault(); ev.stopPropagation();
            var b = pop.querySelectorAll("button")[state.active];
            choose(b.textContent.slice(1));
        } else if (ev.key === "Escape") {
            ev.preventDefault(); ev.stopPropagation();
            close();
        }
    }, true);
    document.addEventListener("focusout", function (ev) { if (state && ev.target === state.ta) close(); });
    window.addEventListener("resize", close);
    document.addEventListener("scroll", function () { if (state) close(); }, true);
})();

// "Ajukan tinjauan" in a file's menu: one popup for every file (an optional note for the reviewer)
document.addEventListener("click", function (ev) {
    var btn = ev.target.closest && ev.target.closest("[data-review]");
    var dlg = document.getElementById("review-dialog");
    if (!btn || !dlg) return;
    ev.preventDefault();
    var form = dlg.querySelector("form");
    form.reset();
    form.action = btn.dataset.review;
    dlg.querySelector(".review-name").textContent = btn.dataset.reviewName || "";
    var menu = btn.closest("details");
    if (menu) menu.open = false;
    dlg.showModal();
    dlg.querySelector("textarea").focus();
});
// Review editor: "Tampilkan" in the open-comments banner ticks the toolbar filter's "Komentar tinjauan" (one filter, in step)
document.addEventListener("change", function (ev) {
    var t = ev.target;
    if (t.matches && t.matches("[data-filter-review]")) {
        var opt = document.querySelector('#filter-form input[name="status"][value="review"]');
        if (opt && opt.checked !== t.checked) {
            opt.checked = t.checked;
            opt.dispatchEvent(new Event("change", { bubbles: true }));
        }
    } else if (t.matches && t.matches('#filter-form input[name="status"][value="review"]')) {
        var box = document.querySelector("[data-filter-review]");
        if (box) box.checked = t.checked;
    }
});

// Penerjemahan: reload the file lists in place, keeping their search, filters and order
window.refreshFileLists = function () {
    ["pf-filter", "my-filter", "member-filter"].forEach(function (id) {
        var f = document.getElementById(id);
        if (f && window.htmx) htmx.trigger(f, "submit");
    });
};

// Forms marked data-ajax (reserve, release, withdraw a review…) are sent in the background: the page stays where it
// is, a toast says what happened and the file lists refresh. data-confirm still asks first (it resubmits on OK).
document.addEventListener("submit", function (ev) {
    var form = ev.target;
    if (!form.hasAttribute || !form.hasAttribute("data-ajax") || ev.defaultPrevented) return;
    ev.preventDefault();
    var menu = form.closest("details.dropdown");
    if (menu) menu.open = false;
    var btn = ev.submitter || form.querySelector("button");
    if (btn) btn.disabled = true;
    fetch(form.action, { method: "POST", body: new FormData(form), credentials: "same-origin", headers: { "X-Requested-With": "fetch" } })
        .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }, function () { return { ok: false, j: {} }; }); })
        .then(function (res) {
            if (!res.ok) throw new Error(res.j.error || "");
            window.toast(res.j.message);
            window.refreshFileLists();
        })
        .catch(function (err) { window.showMessage(err.message || document.body.dataset.error || "Error"); })
        .finally(function () { if (btn) btn.disabled = false; });
});

// sending it stays on the page: the popup closes, a message shows and the file rows refresh with the new status
(function () {
    var dlg = document.getElementById("review-dialog");
    if (!dlg) return;
    var form = dlg.querySelector("form");
    form.addEventListener("submit", function (ev) {
        ev.preventDefault();
        var btn = form.querySelector('button[type="submit"]');
        btn.disabled = true;
        fetch(form.action, { method: "POST", body: new FormData(form), credentials: "same-origin", headers: { "X-Requested-With": "fetch" } })
            .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
            .then(function (res) {
                if (!res.ok) throw new Error(res.j.error || "");
                dlg.close();
                window.toast(res.j.message);
                window.refreshFileLists();
            })
            .catch(function (err) { window.showMessage(err.message || "Error"); })
            .finally(function () { btn.disabled = false; });
    });
})();

// Penerjemahan: stack each section's sticky parts (title, card head, filters, column row):
// every part sticks right below the ones before it in the same section
(function () {
    function layout() {
        // the project tabs stick at the very top: sections stack below them
        var tabs = document.querySelector(".tabs-nav");
        var ts = tabs && getComputedStyle(tabs);
        // the top bar sticks above the tabs (their own top offset)
        var base = ts && ts.position === "sticky" ? (parseFloat(ts.top) || 0) + Math.max(0, tabs.getBoundingClientRect().height - 1) : 0;
        document.querySelectorAll("[data-sticky-section]").forEach(function (sec) {
            var top = base;
            sec.querySelectorAll("[data-stick]").forEach(function (el) {
                el.style.setProperty("--stick-top", top + "px");
                if (getComputedStyle(el.tagName === "THEAD" ? el.querySelector("th") || el : el).position === "sticky") {
                    // exact (fractional) height, minus a pixel of overlap: rounded heights leave
                    // hairline gaps where the rows underneath show through
                    top += Math.max(0, el.getBoundingClientRect().height - 1);
                }
            });
        });
    }
    if (!document.querySelector("[data-sticky-section]")) return;
    // section titles collapse their section; the state is remembered in this browser
    function store(key, collapsed) {
        try { if (collapsed) localStorage.setItem(key, "1"); else localStorage.removeItem(key); } catch (e) { /* storage unavailable */ }
    }
    function setCollapsed(sec, collapsed) {
        sec.classList.toggle("collapsed", collapsed);
        var btn = sec.querySelector(".section-toggle");
        if (btn) btn.setAttribute("aria-expanded", collapsed ? "false" : "true");
    }
    document.querySelectorAll("[data-collapse-key]").forEach(function (sec) {
        var on = false;
        try { on = localStorage.getItem(sec.dataset.collapseKey) === "1"; } catch (e) { /* storage unavailable */ }
        if (on) setCollapsed(sec, true);
    });
    document.addEventListener("click", function (ev) {
        var btn = ev.target.closest && ev.target.closest(".section-toggle");
        var sec = btn && btn.closest("[data-collapse-key]");
        if (!sec) return;
        var collapsed = !sec.classList.contains("collapsed");
        setCollapsed(sec, collapsed);
        store(sec.dataset.collapseKey, collapsed);
        layout();
    });
    layout();
    window.addEventListener("resize", layout);
    window.addEventListener("load", layout);
    document.addEventListener("htmx:afterSettle", layout); // filters and rows can change height
})();


// Profile contribution calendar: on narrow screens show the latest weeks first
document.querySelectorAll(".heat-scroll").forEach(function (el) { el.scrollLeft = el.scrollWidth; });

// Profile: share an earned badge (#share-dialog). Each network gets the badge page's address; its
// preview (title, text, image) comes from the page's Open Graph tags. X also takes the post text.
(function () {
    var dlg = document.getElementById("share-dialog");
    if (!dlg) return;
    var current = null;
    document.addEventListener("click", function (ev) {
        var btn = ev.target.closest && ev.target.closest("[data-share]");
        if (!btn) return;
        var url = btn.getAttribute("data-share"), text = btn.getAttribute("data-share-text") || "";
        var u = encodeURIComponent(url);
        current = { url: url, text: text, title: btn.getAttribute("data-share-title") || "" };
        dlg.querySelector(".share-preview").src = btn.getAttribute("data-share-image");
        dlg.querySelector(".share-text").textContent = text;
        dlg.querySelector(".share-linkedin").href = "https://www.linkedin.com/sharing/share-offsite/?url=" + u;
        dlg.querySelector(".share-x").href = "https://twitter.com/intent/tweet?text=" + encodeURIComponent(text) + "&url=" + u;
        dlg.querySelector(".share-facebook").href = "https://www.facebook.com/sharer/sharer.php?u=" + u;
        dlg.querySelector(".share-copy").setAttribute("data-copy", text + "\n\n" + url); // the post, ready to paste
        dlg.querySelector(".share-open").href = url;
        current.image = btn.getAttribute("data-share-image");
        dlg.querySelector(".share-download").href = current.image + (current.image.indexOf("?") < 0 ? "?" : "&") + "download=1";
        dlg.querySelector(".share-native").hidden = !navigator.share;
        dlg.showModal();
    });
    // the system share sheet gets the picture itself when the device can share files (phones), so it shows
    // in the post whatever the app does with link previews
    dlg.querySelector(".share-native").addEventListener("click", function () {
        if (!current || !navigator.share) return;
        var plain = function () {
            navigator.share({ title: current.title, text: current.text, url: current.url }).catch(function () {});
        };
        if (!navigator.canShare) { plain(); return; }
        fetch(current.image).then(function (r) { return r.blob(); }).then(function (blob) {
            var file = new File([blob], "pelokalan-badge.png", { type: "image/png" });
            var data = { title: current.title, text: current.text + " " + current.url, files: [file] };
            if (navigator.canShare(data)) return navigator.share(data);
            plain();
        }).catch(function () {});
    });
})();


// Profile picture: pick a file, crop it to a square in #avatar-dialog (drag to move, slider/wheel/keys to zoom),
// upload a 512px JPEG (the server makes the stored 256px copy) and update every avatar of the page
(function () {
    var box = document.querySelector("[data-avatar-edit]");
    var dlg = document.getElementById("avatar-dialog");
    if (!box || !dlg) return;
    var input = box.querySelector("#avatar-file");
    var canvas = dlg.querySelector("canvas");
    var ctx = canvas.getContext("2d");
    var zoom = dlg.querySelector('input[type="range"]');
    var S = canvas.width, OUT = 512, MAX = 10 * 1024 * 1024;
    var img = null, base = 1, z = 1, ox = 0, oy = 0, url = null;

    function scale() { return base * z; }
    function clamp() { // the picture always covers the square
        var s = scale(), hw = (img.width * s - S) / 2, hh = (img.height * s - S) / 2;
        ox = Math.max(-hw, Math.min(hw, ox));
        oy = Math.max(-hh, Math.min(hh, oy));
    }
    function draw() {
        if (!img) return;
        clamp();
        var s = scale(), w = img.width * s, h = img.height * s;
        ctx.fillStyle = "#fff";
        ctx.fillRect(0, 0, S, S);
        ctx.drawImage(img, (S - w) / 2 + ox, (S - h) / 2 + oy, w, h);
        // dim the corners outside the round picture
        ctx.save();
        ctx.fillStyle = "rgba(0,0,0,.45)";
        ctx.beginPath();
        ctx.rect(0, 0, S, S);
        ctx.arc(S / 2, S / 2, S / 2 - 1, 0, Math.PI * 2);
        ctx.fill("evenodd");
        ctx.strokeStyle = "rgba(255,255,255,.9)";
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(S / 2, S / 2, S / 2 - 1, 0, Math.PI * 2);
        ctx.stroke();
        ctx.restore();
    }
    function setZoom(nz) {
        var old = scale();
        z = Math.max(1, Math.min(4, nz));
        var k = scale() / old; // keep the middle of the view where it is
        ox *= k; oy *= k;
        zoom.value = z;
        draw();
    }
    function close() {
        if (dlg.open) dlg.close();
        if (url) URL.revokeObjectURL(url);
        url = null; img = null; input.value = "";
    }
    input.addEventListener("change", function () {
        var f = input.files && input.files[0];
        if (!f) return;
        if (!/^image\/(png|jpeg|gif|webp)$/.test(f.type)) { window.showMessage(box.dataset.notImage); input.value = ""; return; }
        if (f.size > MAX) { window.showMessage(box.dataset.tooBig); input.value = ""; return; }
        url = URL.createObjectURL(f);
        var im = new Image();
        im.onload = function () {
            img = im; base = Math.max(S / im.width, S / im.height); z = 1; ox = 0; oy = 0; zoom.value = 1;
            dlg.showModal();
            draw();
            canvas.focus();
        };
        im.onerror = function () { window.showMessage(box.dataset.notImage); close(); };
        im.src = url;
    });
    zoom.addEventListener("input", function () { setZoom(parseFloat(zoom.value)); });
    canvas.addEventListener("wheel", function (e) { e.preventDefault(); setZoom(z * (e.deltaY < 0 ? 1.08 : 1 / 1.08)); }, { passive: false });
    var drag = null;
    canvas.addEventListener("pointerdown", function (e) { drag = { x: e.clientX, y: e.clientY, ox: ox, oy: oy }; canvas.setPointerCapture(e.pointerId); });
    canvas.addEventListener("pointermove", function (e) {
        if (!drag) return;
        var k = S / canvas.getBoundingClientRect().width; // the canvas may be drawn smaller than its pixels
        ox = drag.ox + (e.clientX - drag.x) * k; oy = drag.oy + (e.clientY - drag.y) * k;
        draw();
    });
    canvas.addEventListener("pointerup", function () { drag = null; });
    canvas.addEventListener("pointercancel", function () { drag = null; });
    canvas.addEventListener("keydown", function (e) {
        var step = e.shiftKey ? 30 : 8, used = true;
        if (e.key === "ArrowLeft") ox += step; else if (e.key === "ArrowRight") ox -= step;
        else if (e.key === "ArrowUp") oy += step; else if (e.key === "ArrowDown") oy -= step;
        else if (e.key === "+" || e.key === "=") setZoom(z * 1.1); else if (e.key === "-") setZoom(z / 1.1);
        else used = false;
        if (used) { e.preventDefault(); draw(); }
    });
    dlg.querySelector("[data-avatar-cancel]").addEventListener("click", close);
    dlg.addEventListener("cancel", function () { close(); });

    function showAvatar(src) { // the preview, the top bar and every other avatar of this person on the page
        var prev = box.querySelector(".avatar-preview");
        prev.innerHTML = "";
        if (src) {
            var i = document.createElement("img");
            i.className = "user-avatar"; i.alt = ""; i.src = src;
            prev.appendChild(i);
        } else {
            var d = document.createElement("div");
            d.className = "user-avatar"; d.setAttribute("aria-hidden", "true");
            d.textContent = (document.querySelector(".usermenu .avatar") || {}).textContent || "?";
            prev.appendChild(d);
        }
        var nav = document.querySelector(".usermenu summary .avatar");
        if (nav) {
            var n;
            if (src) { n = document.createElement("img"); n.className = "avatar"; n.alt = ""; n.src = src; }
            else { n = document.createElement("span"); n.className = "avatar"; n.setAttribute("aria-hidden", "true"); n.textContent = prev.textContent.trim(); }
            nav.replaceWith(n);
        }
        box.querySelector("[data-avatar-remove]").hidden = !src;
    }
    var saveBtn = dlg.querySelector("[data-avatar-save]");
    saveBtn.addEventListener("click", function () {
        if (!img) return;
        clamp();
        var s = scale(), side = S / s;
        var sx = (img.width - side) / 2 - ox / s, sy = (img.height - side) / 2 - oy / s;
        var out = document.createElement("canvas");
        out.width = out.height = OUT;
        var o = out.getContext("2d");
        o.fillStyle = "#fff";
        o.fillRect(0, 0, OUT, OUT);
        o.imageSmoothingQuality = "high";
        o.drawImage(img, sx, sy, side, side, 0, 0, OUT, OUT);
        saveBtn.disabled = true;
        out.toBlob(function (blob) {
            var fd = new FormData();
            fd.append("avatar", blob, "avatar.jpg");
            fetch(box.dataset.upload, { method: "POST", body: fd, credentials: "same-origin" })
                .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
                .then(function (res) {
                    if (!res.ok) throw new Error(res.j.error || "");
                    showAvatar(res.j.url);
                    close();
                    window.toast(box.dataset.saved);
                })
                .catch(function (err) { window.showMessage(err.message || box.dataset.notImage); })
                .finally(function () { saveBtn.disabled = false; });
        }, "image/jpeg", 0.9);
    });
    box.querySelector("[data-avatar-remove]").addEventListener("click", function () {
        window.askConfirm(box.dataset.removeConfirm, { danger: true }).then(function (ok) {
            if (!ok) return;
            fetch(box.dataset.delete, { method: "POST", credentials: "same-origin" })
                .then(function (r) { if (!r.ok) throw new Error(); showAvatar(""); window.toast(box.dataset.removed); })
                .catch(function () { window.showMessage(box.dataset.notImage); });
        });
    });
})();

// "Tarik upstream" ([data-merge-upstream]: the file's /upstream-merge address): the merge is planned first (nothing
// changes), the popup shows what it would do and every string changed on both sides with a choice; "Gabungkan"
// applies it once each has one. A changed upstream since the check asks to check again.
(function () {
    var dlg = document.getElementById("merge-dialog");
    if (!dlg) return;
    var body = dlg.querySelector(".merge-body");
    var apply = dlg.querySelector("[data-merge-apply]");
    var recheck = dlg.querySelector("[data-merge-recheck]");
    var url = "", plan = null;

    function el(tag, cls, text) {
        var e = document.createElement(tag);
        if (cls) e.className = cls;
        if (text != null) e.textContent = text;
        return e;
    }
    function count(tpl, n) { return tpl.replace(/\d+/, String(n)); }
    function forms(list, fuzzy) {
        var box = el("div", "merge-text");
        (list && list.length ? list : [""]).forEach(function (t, i) {
            var line = el("div", "merge-form", t === "" ? "—" : t);
            if (list && list.length > 1) line.setAttribute("data-form", "[" + i + "]");
            box.appendChild(line);
        });
        if (fuzzy) box.appendChild(el("span", "merge-fuzzy", dlg.dataset.fuzzy));
        return box;
    }
    function ready() {
        if (!plan) { apply.disabled = true; return; }
        var missing = plan.conflicts.some(function (c) { return !body.querySelector('input[name="c_' + c.key + '"]:checked'); });
        var nothing = !plan.updated && !plan.added && !plan.removed && !plan.conflicts.length;
        apply.disabled = missing || nothing;
    }
    function render() {
        body.innerHTML = "";
        body.appendChild(el("p", "small muted", dlg.dataset.source.replace("%s", plan.label || "")));
        var ul = el("ul", "merge-summary");
        if (plan.updated + plan.added) ul.appendChild(el("li", "", count(dlg.dataset.updated, plan.updated + plan.added)));
        if (plan.kept) ul.appendChild(el("li", "", count(dlg.dataset.kept, plan.kept)));
        if (plan.removed) ul.appendChild(el("li", "", count(dlg.dataset.removed, plan.removed)));
        if (ul.children.length) body.appendChild(ul);
        if (!plan.updated && !plan.added && !plan.removed && !plan.conflicts.length) {
            body.appendChild(el("p", "merge-nothing", dlg.dataset.nothing));
        }
        if (plan.conflicts.length) {
            body.appendChild(el("p", "merge-conflict-head", count(dlg.dataset.conflicts, plan.conflicts.length)));
            plan.conflicts.forEach(function (c) {
                var item = el("div", "merge-conflict");
                var src = el("div", "merge-src");
                if (c.context) src.appendChild(el("span", "ctx-tag", c.context));
                src.appendChild(document.createTextNode(c.source));
                item.appendChild(src);
                var opts = el("div", "merge-opts");
                [["upstream", dlg.dataset.pickUpstream, c.upstream, c.upstream_fuzzy], ["mine", dlg.dataset.pickMine, c.mine, c.mine_fuzzy]].forEach(function (o) {
                    var label = el("label", "merge-opt");
                    var input = el("input");
                    input.type = "radio"; input.name = "c_" + c.key; input.value = o[0];
                    label.appendChild(input);
                    var head = el("span", "merge-opt-head", o[1]);
                    label.appendChild(head);
                    label.appendChild(forms(o[2], o[3]));
                    opts.appendChild(label);
                });
                item.appendChild(opts);
                body.appendChild(item);
            });
        }
        ready();
    }
    function load() {
        plan = null;
        apply.disabled = true;
        recheck.hidden = true;
        body.innerHTML = "";
        body.appendChild(el("p", "muted", dlg.dataset.loading));
        fetch(url, { credentials: "same-origin", headers: { "X-Requested-With": "fetch" } })
            .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
            .then(function (res) {
                if (!res.ok) throw new Error(res.j.error || "");
                plan = res.j;
                plan.conflicts = plan.conflicts || [];
                render();
            })
            .catch(function (err) {
                body.innerHTML = "";
                body.appendChild(el("p", "alert alert-error", err.message || "Error"));
                recheck.hidden = false;
            });
    }
    document.addEventListener("click", function (ev) {
        var btn = ev.target.closest && ev.target.closest("[data-merge-upstream]");
        if (!btn) return;
        ev.preventDefault();
        url = btn.getAttribute("data-merge-upstream");
        var menu = btn.closest("details");
        if (menu) menu.open = false;
        dlg.showModal();
        load();
    });
    body.addEventListener("change", ready);
    recheck.addEventListener("click", load);
    apply.addEventListener("click", function () {
        if (!plan) return;
        var fd = new FormData();
        fd.append("version", plan.version);
        body.querySelectorAll("input[type=radio]:checked").forEach(function (i) { fd.append(i.name, i.value); });
        apply.disabled = true;
        fetch(url, { method: "POST", body: fd, credentials: "same-origin", headers: { "X-Requested-With": "fetch" } })
            .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
            .then(function (res) {
                if (!res.ok) throw new Error(res.j.error || "");
                // the file changed everywhere: reload the page, with the summary as a toast
                try { sessionStorage.setItem("pendingToast", res.j.message); } catch (e) { /* storage off */ }
                location.reload();
            })
            .catch(function (err) {
                body.insertBefore(el("p", "alert alert-error", err.message || "Error"), body.firstChild);
                recheck.hidden = false;
                ready();
            });
    });
})();

// a toast a page left for the next one (after a reload)
(function () {
    var msg = null;
    try { msg = sessionStorage.getItem("pendingToast"); sessionStorage.removeItem("pendingToast"); } catch (e) { /* storage off */ }
    if (msg && window.toast) window.toast(msg);
})();

// Menunggu tinjauan: search (every word in the file name or the translator) and order, in the page (the list is short)
(function () {
    var form = document.querySelector("[data-review-queue]");
    if (!form) return;
    var list = form.closest(".review-queue").querySelector(".review-list");
    var none = form.closest(".review-queue").querySelector(".rq-none");
    var items = Array.prototype.slice.call(list.children);
    var key = "reviewQueueSort";
    try { var saved = localStorage.getItem(key); if (saved) form.s.value = saved; } catch (e) { /* storage off */ }
    function num(li, k) { return parseFloat(li.dataset[k]) || 0; }
    var orders = {
        oldest: function (a, b) { return num(a, "t") - num(b, "t"); },
        newest: function (a, b) { return num(b, "t") - num(a, "t"); },
        fixed: function (a, b) { return (a.dataset.state === "1" ? 0 : 1) - (b.dataset.state === "1" ? 0 : 1) || num(a, "t") - num(b, "t"); },
        most: function (a, b) { return num(b, "n") - num(a, "n"); },
        fewest: function (a, b) { return num(a, "n") - num(b, "n"); },
        name: function (a, b) { return a.dataset.name.localeCompare(b.dataset.name); },
        user: function (a, b) { return a.dataset.user.localeCompare(b.dataset.user) || num(a, "t") - num(b, "t"); }
    };
    function apply() {
        var words = form.q.value.toLowerCase().split(/\s+/).filter(Boolean);
        var shown = 0;
        items.slice().sort(orders[form.s.value] || orders.oldest).forEach(function (li) {
            var hay = (li.dataset.name + " @" + li.dataset.user).toLowerCase();
            var ok = words.every(function (w) { return hay.indexOf(w) >= 0; });
            li.hidden = !ok;
            if (ok) shown++;
            list.appendChild(li); // in the chosen order
        });
        if (none) none.hidden = shown > 0;
    }
    form.addEventListener("submit", function (ev) { ev.preventDefault(); });
    form.q.addEventListener("input", apply);
    form.s.addEventListener("change", function () {
        try { localStorage.setItem(key, form.s.value); } catch (e) { /* storage off */ }
        apply();
    });
    apply();
})();

// Review editor: "Komentari bagian ini" (from the selection popup) comments on a passage of a translation. The
// row is selected (its thread loads in the side panel), the passage goes into the comment form as a quote, and
// after sending the rows are reloaded so the passage gets its pink mark.
(function () {
    var anchoredSend = false;
    function fill(form, info) {
        var box = form.querySelector(".comment-anchor");
        if (!box) return;
        box.hidden = false;
        box.querySelector(".comment-anchor-text").textContent = info.text;
        [["anchor_text", info.text], ["anchor_start", info.start], ["anchor_form", info.form]].forEach(function (kv) {
            var inp = box.querySelector('input[name="' + kv[0] + '"]');
            inp.value = String(kv[1]);
            inp.disabled = false;
        });
        var ta = form.querySelector("textarea[name=body]");
        if (ta) ta.focus();
    }
    function sidebarForm() { return document.querySelector("#sidebar .thread .comment-form"); }
    window.startAnchorComment = function (info) {
        var sel = window.getSelection();
        if (sel) sel.removeAllRanges();
        // the side panel must be open to write (tablets and phones keep it closed)
        var ws = document.querySelector(".workspace");
        var toggle = document.getElementById("side-toggle");
        if (ws && toggle && (ws.classList.contains("sidebar-hidden") || ws.classList.contains("sidebar-closed")) && getComputedStyle(toggle).display !== "none") toggle.click();
        var old = sidebarForm();
        if (info.row.classList.contains("selected") && old) { fill(old, info); return; }
        if (old) old.dataset.stale = "1";
        info.row.click();
        var tries = 0, wait = setInterval(function () {
            var f = sidebarForm();
            if (f && !f.dataset.stale) { clearInterval(wait); fill(f, info); }
            else if (++tries > 60) clearInterval(wait);
        }, 100);
    };
    document.addEventListener("click", function (ev) {
        var clear = ev.target.closest && ev.target.closest("[data-anchor-clear]");
        if (!clear) return;
        var box = clear.closest(".comment-anchor");
        box.hidden = true;
        box.querySelectorAll("input").forEach(function (i) { i.value = ""; i.disabled = true; });
    });
    document.body.addEventListener("htmx:beforeRequest", function (ev) {
        var f = ev.target && ev.target.matches && ev.target.matches(".comment-form") ? ev.target : null;
        if (f) anchoredSend = !!f.querySelector('.comment-anchor input[name="anchor_text"]:not([disabled])');
    });
    document.body.addEventListener("htmx:afterSettle", function (ev) {
        if (!anchoredSend || !ev.target || !ev.target.closest || !ev.target.closest(".thread, #sidebar")) return;
        anchoredSend = false;
        var form = document.getElementById("filter-form");
        if (form && window.htmx) htmx.trigger(form, "submit"); // the new mark
    });
})();

// Glossary extraction from long strings ("Ekstrak istilah dengan AI" on the glossary page): the dialog shows the
// estimate for the chosen module group and starts the run; the toolbar shows its progress until it ends, then the
// results reload.
(function () {
    var dlg = document.getElementById("extract-dialog");
    var opener = document.querySelector("[data-extract-open]");
    if (!dlg || !opener) return;
    var base = dlg.dataset.base;
    var status = document.getElementById("extract-status");
    var group = dlg.querySelector("[data-extract-group]");
    var box = dlg.querySelector("[data-extract-estimate]");
    var start = dlg.querySelector("[data-extract-start]");
    var stop = dlg.querySelector("[data-extract-cancel]");
    var running = false, timer = null, lastID = null;

    function post(url, body) {
        return fetch(url, {method: "POST", body: body, credentials: "same-origin",
            headers: {"X-Requested-With": "fetch"}}).then(function (r) { return r.json(); });
    }
    function line(text, cls) {
        var p = document.createElement("p");
        p.textContent = text;
        if (cls) p.className = cls;
        box.appendChild(p);
    }
    function estimate() {
        box.textContent = dlg.dataset.starting;
        start.disabled = true;
        fetch(base + "?group=" + encodeURIComponent(group.value), {credentials: "same-origin"})
            .then(function (r) { return r.json(); })
            .then(function (d) {
                box.textContent = "";
                if (d.error) { line(d.error, "c-err"); return; }
                if (!d.configured) { line(dlg.dataset.noai || "", "c-err"); return; }
                line(d.summary);
                if (d.cost) line(d.cost);
                if (d.budget) line(d.budget, d.over_budget ? "c-err" : "muted");
                start.disabled = running || d.over_budget;
            })
            .catch(function () { box.textContent = ""; });
    }
    function showRunning(on) {
        running = on;
        stop.hidden = !on;
        if (on) start.disabled = true;
    }
    function poll() {
        clearTimeout(timer);
        fetch(base + "/status", {credentials: "same-origin", cache: "no-store"})
            .then(function (r) { return r.json(); })
            .then(function (d) {
                var was = running;
                showRunning(!!d.running);
                if (d.running) {
                    status.hidden = false;
                    status.classList.remove("c-err");
                    status.textContent = d.message;
                    timer = setTimeout(poll, 3000);
                    return;
                }
                if (was && d.id === lastID) { // the run this page watched has ended
                    if (d.error) window.toast(d.error, true); else window.toast(d.message);
                    status.hidden = true;
                    var form = document.getElementById("glossary-form");
                    if (form && window.htmx) htmx.trigger(form, "submit");
                }
            })
            .catch(function () { timer = setTimeout(poll, 10000); });
    }
    function watch() {
        fetch(base + "/status", {credentials: "same-origin", cache: "no-store"})
            .then(function (r) { return r.json(); })
            .then(function (d) { lastID = d.id; if (d.running) { showRunning(true); poll(); } });
    }

    opener.addEventListener("click", function () {
        if (typeof dlg.showModal === "function") dlg.showModal(); else dlg.setAttribute("open", "");
        estimate();
    });
    group.addEventListener("change", estimate);
    start.addEventListener("click", function () {
        start.disabled = true;
        var fd = new FormData();
        fd.append("group", group.value);
        post(base, fd).then(function (d) {
            if (d.error) { window.toast(d.error, true); start.disabled = false; return; }
            window.toast(d.message);
            dlg.close();
            lastID = null;
            fetch(base + "/status", {credentials: "same-origin", cache: "no-store"})
                .then(function (r) { return r.json(); })
                .then(function (s) { lastID = s.id; showRunning(true); poll(); });
        });
    });
    stop.addEventListener("click", function () { post(base + "/cancel", new FormData()).then(poll); });
    watch();
})();
