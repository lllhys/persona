"""Run through browser-use stdin against TestSkillImportBrowserHarness and Vite.

PERSONA_SKILL_ZIP_ORIGIN selects the loopback web origin. PERSONA_SKILL_ZIP_SHOTS
selects the evidence directory; PERSONA_SKILL_ZIP_WORK selects scratch storage.
The Go fixture uses its real importer in a disposable directory, never a live hub.
"""
import hashlib
import json
import os
import subprocess
import tempfile
import time
import zipfile
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlparse

repo = Path.cwd()
origin = os.environ["PERSONA_SKILL_ZIP_ORIGIN"].rstrip("/")
assert urlparse(origin).hostname in ("127.0.0.1", "localhost"), "Use an owned loopback fixture"
shots = Path(os.environ["PERSONA_SKILL_ZIP_SHOTS"])
shots.mkdir(parents=True, exist_ok=True)
scratch = tempfile.TemporaryDirectory(dir=os.environ["PERSONA_SKILL_ZIP_WORK"], prefix="skill-zip-")
fixtures = Path(scratch.name)
for name, description in (("example-visual", "Summarize a supplied report and list the next steps."),
                          ("example-ignored", "An extra ZIP that must not replace a pending check."),
                          ("example-invalid", "")):
    with zipfile.ZipFile(fixtures / (name + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr(name + "/SKILL.md", f"---\nname: {name}\ndescription: {description}\n---\n\nSummarize the supplied report.\n")
        archive.writestr(name + "/references/example.txt", "Synthetic visual-verification material.\n")

captures = []
checks = []
edge_taps = []
language, theme, width, text_size = "en", "light", 1280, "standard"


def wait_js(expression):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        if js(expression):
            return
        time.sleep(.1)
    raise AssertionError("Timed out: " + expression)


def labels():
    return json.loads((repo / "web/src/i18n/locales" / language / "settings-skills.json").read_text())


def settle_dialog():
    # Native edge taps need the sheet's final coordinates, including after row Undo.
    wait_js("Array.from(document.querySelectorAll('[role=dialog]')).every(d=>d.getAnimations().every(a=>a.playState!=='running'))")


def click_label(text):
    # Resolve a rendered accessible button, then send a real pointer click.
    js("Array.from(document.querySelectorAll('button')).find(e=>e.textContent===" + json.dumps(text) + ").scrollIntoView({block:'center'})")
    settle_dialog()
    node = next(n for n in cdp("Accessibility.getFullAXTree")["nodes"]
                if n.get("role", {}).get("value") == "button" and n.get("name", {}).get("value") == text)
    box = cdp("DOM.getBoxModel", backendNodeId=node["backendDOMNodeId"])["model"]["content"]
    if js("matchMedia('(pointer:coarse)').matches"):
        # Tap the expanded target below the 32 px painted button.
        rect = js("Array.from(document.querySelectorAll('button')).find(e=>e.textContent===" + json.dumps(text) + ").getBoundingClientRect().toJSON()")
        point = {"x": rect["x"] + rect["width"] / 2, "y": rect["y"] + rect["height"] / 2 + 21}
        cdp("Input.dispatchTouchEvent", type="touchStart", touchPoints=[point])
        cdp("Input.dispatchTouchEvent", type="touchEnd", touchPoints=[])
        edge_taps.append(dict(label=text, locale=language, width=width, text_size=text_size))
    else:
        click_at_xy(sum(box[0::2]) / 4, sum(box[1::2]) / 4)


def appearance(dark, size=1280, large=False):
    global theme, width, text_size
    theme, width, text_size = "dark" if dark else "light", size, "large" if large else "standard"
    cdp("Emulation.setDeviceMetricsOverride", width=size, height=900 if size > 500 else 844,
        deviceScaleFactor=1, mobile=size < 500)
    cdp("Emulation.setTouchEmulationEnabled", enabled=size < 500)
    js("document.documentElement.classList.toggle('dark'," + json.dumps(dark) + ");document.documentElement.dataset.textSize=" + json.dumps(text_size))


def configure(locale):
    global language
    if js("!!document.querySelector('[role=dialog]')"):
        press_key("Escape")
        wait_js("!document.querySelector('[role=dialog]')")
    language = locale
    js("localStorage.setItem('persona.token','mock');localStorage.setItem('persona.language'," + json.dumps(locale) + ")")
    new_tab(origin + "/assistant")
    wait_for_load()
    wait_js("document.querySelector('[data-settings-group=skills]')?.textContent.includes('Example computer')")
    js("""(()=>{window.__visual={calls:[],replies:[],events:[],errors:[],hold:false};
      addEventListener('error',e=>__visual.errors.push(e.message));
      addEventListener('unhandledrejection',e=>__visual.errors.push(String(e.reason)));
      for(const type of ['dragover','drop']) document.addEventListener(type,e=>{
        if(e.target.closest('[data-testid=skill-zip-drop]'))
          __visual.events.push({type,prevented:e.defaultPrevented});
      });
      const original=window.fetch;
      window.fetch=async function(input,init){
        if(!String(input).endsWith('/skills/import')) return original.apply(this,arguments);
        const body=JSON.parse(init.body);__visual.calls.push(body.op);
        const reply=await original.apply(this,arguments);
        __visual.replies.push({op:body.op,status:reply.status,body:await reply.clone().json()});
        if(body.op==='preview') while(__visual.hold) await new Promise(r=>setTimeout(r,20));
        return reply;
      };})()""")


def open_upload():
    click_label(labels()["upload"])
    wait_js("!!document.querySelector('input[type=file]')")
    settle_dialog()


def upload(name="example-visual"):
    root = cdp("DOM.getDocument")["root"]["nodeId"]
    node = cdp("DOM.querySelector", nodeId=root, selector="input[type=file]")["nodeId"]
    cdp("DOM.setFileInputFiles", nodeId=node, files=[str(fixtures / (name + ".zip"))])


def preview():
    wait_js("document.querySelector('[role=dialog]')?.textContent.includes(" + json.dumps(labels()["valid"]) + ")")


def capture(name, state):
    time.sleep(.2)  # Allow the existing dialog/color transition to settle.
    measurement = js("""(()=>{const d=document.querySelector('[role=dialog]'),scope=d??document.querySelector('[data-settings-group=skills]');return {
      pageOverflow:document.documentElement.scrollWidth>innerWidth,
      dialogOverflow:d?d.scrollWidth>d.clientWidth:false,
      coarsePointer:matchMedia('(pointer:coarse)').matches,
      buttons:Array.from(scope?.querySelectorAll('button')??[]).map(e=>{
        const r=e.getBoundingClientRect(), p=getComputedStyle(e,'::before');
        const inset=v=>Math.min(0,parseFloat(v)||0), expanded=p.content!=='none'&&p.content!=='normal';
        const x=r.x+r.width/2,y=r.y+r.height/2;
        return {label:e.textContent,visible:r.left>=0&&r.right<=innerWidth&&r.top>=0&&r.bottom<=innerHeight,
          hitWidth:r.width-(expanded?inset(p.left)+inset(p.right):0),
          hitHeight:r.height-(expanded?inset(p.top)+inset(p.bottom):0),
          hits44:[[-21.5,0],[21.5,0],[0,-21.5],[0,21.5]].every(([dx,dy])=>document.elementFromPoint(x+dx,y+dy)?.closest('button')===e)}
      }),errors:__visual.errors}})()""")
    assert not measurement["pageOverflow"] and not measurement["dialogOverflow"], measurement
    assert all(b["visible"] for b in measurement["buttons"]), measurement
    if measurement["coarsePointer"]:
        assert all(b["hitWidth"] >= 44 and b["hitHeight"] >= 44 and b["hits44"] for b in measurement["buttons"]), measurement
    assert not measurement["errors"], measurement
    capture_screenshot(str(shots / (name + ".png")))
    captures.append(dict(file=name + ".png", state=state, locale=language, theme=theme,
                         width=width, text_size=text_size, **measurement))
    print("PASS capture", name, flush=True)


new_tab(origin + "/assistant")
wait_for_load()
print(page_info())
configure("en")
appearance(False)
open_upload()
print(js("document.querySelector('[role=dialog]').innerText"))
for mime, text in (("text/uri-list", "https://example.com/"), ("text/plain", "Synthetic dragged text")):
    zone = js("document.querySelector('[data-testid=skill-zip-drop]').getBoundingClientRect().toJSON()")
    x, y = zone["x"] + zone["width"] / 2, zone["y"] + zone["height"] / 2
    data = dict(items=[dict(mimeType=mime, data=text)], files=[], dragOperationsMask=1)
    for event in ("dragEnter", "dragOver", "drop"):
        cdp("Input.dispatchDragEvent", type=event, x=x, y=y, data=data)
    assert js("__visual.events.slice(-2)") == [{"type": "dragover", "prevented": True}, {"type": "drop", "prevented": True}]
    assert page_info()["url"] == origin + "/assistant"
    assert js("__visual.calls.length") == 0
    assert not js("document.querySelector('[data-testid=skill-zip-drop]').className.includes('border-accent')")
    assert js("!!document.querySelector('input[type=file]')")
checks.append("Native link/text dragover and drop cancelled without navigation, highlight or upload")
upload()
preview()
assert js("document.activeElement.textContent") == "Cancel"
capture("preview-light", "preview; Cancel initially focused")
press_key("Tab")
assert js("document.activeElement.textContent") == labels()["install"]
appearance(True)
capture("preview-dark", "preview; Install focused by Tab")
appearance(True, 390, True)
capture("narrow-dark-large", "preview; Large text")
appearance(False, 390, True)
capture("narrow-light-large", "preview; Large text")
press_key("Escape")
wait_js("!document.querySelector('[role=dialog]')")
assert js("document.activeElement.textContent") == labels()["upload"]
checks.append("Cancel initially focused; Tab reaches Install; Escape restores Upload ZIP focus")

for locale, prefix in (("ja", "ja"), ("zh-Hans", "zh")):
    configure(locale)
    appearance(False, 390)
    open_upload()
    upload()
    preview()
    capture(prefix + "-preview", "localized preview")
    appearance(True, 390)
    capture(prefix + "-preview-dark", "localized preview")

configure("zh-Hans")
appearance(False)
js("document.querySelector('[data-settings-group=skills]').scrollIntoView({block:'center'})")
capture("zh-group-light", "Skills group")
appearance(True)
capture("zh-group-dark", "Skills group")
open_upload()
appearance(False, 390)
capture("zh-choose-light", "choose ZIP")
js("__visual.chooserClicks=0;document.querySelector('input[type=file]').addEventListener('click',e=>{__visual.chooserClicks++;e.preventDefault()})")
click_label(labels()["file_label"])
assert js("__visual.chooserClicks") == 1
cancel = js("Array.from(document.querySelectorAll('[role=dialog] button')).at(-1).textContent")
click_label(cancel)
wait_js("!document.querySelector('[role=dialog]')")
assert js("document.activeElement.textContent") == labels()["upload"]
open_upload()
appearance(True, 390)
zone = js("document.querySelector('[data-testid=skill-zip-drop]').getBoundingClientRect().toJSON()")
data = dict(items=[], files=[str(fixtures / "example-visual.zip")], dragOperationsMask=1)
x, y = zone["x"] + zone["width"] / 2, zone["y"] + zone["height"] / 2
cdp("Input.dispatchDragEvent", type="dragEnter", x=x, y=y, data=data)
cdp("Input.dispatchDragEvent", type="dragOver", x=x, y=y, data=data)
wait_js("document.querySelector('[data-testid=skill-zip-drop]').className.includes('border-accent')")
capture("zh-drop-dark", "native file drag over the zone")
js("__visual.hold=true")
cdp("Input.dispatchDragEvent", type="drop", x=x, y=y, data=data)
wait_js("__visual.calls.includes('preview')")
assert js("document.querySelector('[data-testid=skill-zip-drop]').textContent.includes('example-visual.zip')")
capture("zh-checking-dark", "pending real preview response")
appearance(False, 390)
capture("zh-checking-light", "pending real preview response")
before = js("__visual.calls.length")
data["files"] = [str(fixtures / "example-ignored.zip")]
for event in ("dragEnter", "dragOver", "drop"):
    cdp("Input.dispatchDragEvent", type=event, x=x, y=y, data=data)
assert js("__visual.events.slice(-2)") == [{"type": "dragover", "prevented": True}, {"type": "drop", "prevented": True}]
assert js("__visual.calls.length") == before
assert not js("document.querySelector('[data-testid=skill-zip-drop]').className.includes('border-accent')")
assert js("document.querySelector('[data-testid=skill-zip-drop]').textContent.includes('example-visual.zip')")
assert page_info()["url"] == origin + "/assistant"
js("__visual.hold=false")
preview()
checks.append("Native dragover/drop cancelled during a held real preview; no navigation, second request or changed filename")
press_key("Escape")
wait_js("!document.querySelector('[role=dialog]')")
open_upload()
upload("example-invalid")
wait_js("!!document.querySelector('[role=dialog] [role=alert]')")
assert js("document.querySelector('[role=dialog] [role=alert]').textContent") == labels()["error_description"]
capture("zh-refused-light", "real importer refuses an empty description")
appearance(True, 390)
capture("zh-refused-dark", "real importer refuses an empty description")
upload()
preview()
click_label(labels()["install"])
wait_js("document.querySelector('[role=dialog]')?.textContent.includes(" + json.dumps(labels()["installed"].replace("{name}", "example-visual")) + ")")
appearance(False, 390)
capture("zh-installed-light", "installed in the real temporary store")
appearance(True, 390)
capture("zh-installed-dark", "installed in the real temporary store")
destination = Path(js("__visual.replies.findLast(r=>r.op==='install').body.destination"))
assert (destination / "SKILL.md").is_file()
click_label(labels()["undo"])
wait_js("document.querySelector('[role=dialog]')?.textContent.includes(" + json.dumps(labels()["undone_title"]) + ")")
capture("zh-undone-dark", "real Undo; recovery path retained")
appearance(False, 390)
capture("zh-undone-light", "real Undo; recovery path retained")
recovery = Path(js("__visual.replies.findLast(r=>r.op==='undo').body.recovery"))
assert not destination.exists() and (recovery / "SKILL.md").is_file()
click_label(labels()["close"])
wait_js("!document.querySelector('[role=dialog]')")
wait_js("!document.querySelector('[data-settings-group=skills]').textContent.includes('example-visual')")
checks.append("Explicit Install creates SKILL.md; Undo retains it in recovery and refreshes the list")
checks.append("The real importer rejects an empty description with the localized actionable error")

# Install once more to exercise the separate post-install Undo row with a finger.
open_upload()
upload()
preview()
click_label(labels()["install"])
wait_js("document.querySelector('[role=dialog]')?.textContent.includes(" + json.dumps(labels()["installed"].replace("{name}", "example-visual")) + ")")
click_label(labels()["close"])
wait_js("!document.querySelector('[role=dialog]')")
appearance(True, 390, True)
js("document.querySelector('[data-settings-group=skills]').scrollIntoView({block:'center'})")
capture("zh-undo-row-dark", "post-install Undo row; coarse pointer and Large text")
appearance(False, 390, True)
capture("zh-undo-row-light", "post-install Undo row; coarse pointer and Large text")
click_label(labels()["undo"])
wait_js("document.querySelector('[role=dialog]')?.textContent.includes(" + json.dumps(labels()["undone_title"]) + ")")
recovery = Path(js("__visual.replies.findLast(r=>r.op==='undo').body.recovery"))
assert not destination.exists() and (recovery / "SKILL.md").is_file()
click_label(labels()["close"])
wait_js("!document.querySelector('[role=dialog]')")
wait_js("!document.querySelector('[data-settings-group=skills]').textContent.includes('example-visual')")
checks.append("Coarse-pointer targets span at least 44 px in both axes and hit-test across that area")
checks.append("Native taps outside the painted buttons activate Upload, Choose ZIP, Cancel, Install, Close and both Undo actions")

sources = ["web/src/views/settings/Skills.tsx", "web/e2e/skill-zip-visual.py", "server/internal/assistant/skills_import_browser_test.go"]
sources += ["web/src/i18n/locales/" + lang + "/settings-skills.json" for lang in ("en", "zh-Hans", "ja")]
report = dict(source_commit=subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
              captured_at=datetime.now(timezone.utc).isoformat(),
              browser=cdp("Browser.getVersion")["product"],
              sources={name: hashlib.sha256((repo / name).read_bytes()).hexdigest() for name in sources},
              checks=checks, captures=captures, edge_taps=edge_taps,
              screenshots={item["file"]: hashlib.sha256((shots / item["file"]).read_bytes()).hexdigest() for item in captures},
              manual_inspection="pending; browser assertions do not constitute visual approval")
(shots / "verification.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
scratch.cleanup()
print("PASS Skills ZIP browser checks:", len(captures), "captures;", len(checks), "behavior checks")
