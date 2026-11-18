"""Run via browser-use stdin against scripts/mock/skill_mentions.py and the real Vite app.

Set PERSONA_SKILL_SHOTS to a local evidence directory. Screenshots use synthetic data only.
The browser-use CLI preloads the interaction helpers used below.
"""
import os
import time
import urllib.request
from pathlib import Path

shots = Path(os.environ["PERSONA_SKILL_SHOTS"])
shots.mkdir(parents=True, exist_ok=True)
control = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def state(value):
    control.open(f"http://127.0.0.1:25357/mock/skills?state={value}").read()


def shot(name):
    time.sleep(.3)  # let the existing sliding highlight settle
    capture_screenshot(str(shots / f"{name}.png"))


new_tab("http://127.0.0.1:25358/persona/new")
wait_for_load()
js("localStorage.setItem('persona.token','mock'); localStorage.setItem('persona.language','en'); location.reload()")
wait_for_element("textarea", timeout=15)
print(page_info())
print(js("Array.from(document.querySelectorAll('textarea')).map(e=>({placeholder:e.placeholder,disabled:e.disabled}))"))
cdp("Emulation.setDeviceMetricsOverride", width=1365, height=900, deviceScaleFactor=1, mobile=False)
js("document.documentElement.classList.remove('dark')")
fill_input("textarea", "@review")
time.sleep(.4)
rows = js("Array.from(document.querySelectorAll('[role=option]')).map(e=>({text:e.textContent,selected:e.getAttribute('aria-selected')}))")
print("Rendered duplicate sources:", rows)
assert len(rows) == 2
press_key("ArrowDown")
shot("light-menu")
js("document.documentElement.classList.add('dark')")
shot("dark-menu")
cdp("Emulation.setDeviceMetricsOverride", width=390, height=844, deviceScaleFactor=1, mobile=True)
cdp("Emulation.setTouchEmulationEnabled", enabled=True)
js("document.documentElement.setAttribute('data-text-size','large')")
shot("narrow-dark-menu")
js("document.documentElement.classList.remove('dark')")
shot("narrow-light-menu")
js("document.documentElement.classList.add('dark')")
assert js("document.documentElement.scrollWidth <= innerWidth")
# Keyboard select first, then verify a real emulated touch can remove it.
press_key("Enter")
fill_input("textarea", "Check the example")
time.sleep(.4)
assert js("localStorage.getItem('draft:persona-new').includes('fixture-user')")
shot("narrow-selected")
js("document.documentElement.classList.remove('dark')")
shot("narrow-light-selected")
js("document.documentElement.classList.add('dark')")
node = next(n for n in cdp("Accessibility.getFullAXTree")["nodes"]
            if n.get("role", {}).get("value") == "button" and n.get("name", {}).get("value") == "Remove this context")
q = cdp("DOM.getBoxModel", backendNodeId=node["backendDOMNodeId"])["model"]["content"]
x, y = sum(q[0::2]) / 4, sum(q[1::2]) / 4
assert max(q[0::2]) - min(q[0::2]) >= 44
assert max(q[1::2]) - min(q[1::2]) >= 44
cdp("Input.dispatchTouchEvent", type="touchStart", touchPoints=[{"x": x, "y": y}])
cdp("Input.dispatchTouchEvent", type="touchEnd", touchPoints=[])
assert not js("document.body.innerText.includes('review · user · ~/example/skills')")
state("loading")
fill_input("textarea", "@security")
time.sleep(.1)
print("Loading:", js("Array.from(document.querySelectorAll('[role=status]')).map(e=>e.textContent)"))
shot("narrow-loading")
time.sleep(2)
press_key("Escape")
fill_input("textarea", "Ordinary chat")
state("error")
fill_input("textarea", "@review")
time.sleep(.4)
print("Failure:", js("Array.from(document.querySelectorAll('[role=status]')).map(e=>e.textContent)"))
shot("narrow-error")
assert js("document.body.innerText.includes('Skills unavailable here')")
press_key("Escape")
fill_input("textarea", "Plain chat remains usable")
assert not js("document.querySelector('textarea').disabled")
state("partial")
fill_input("textarea", "@security")
time.sleep(.4)
js("document.documentElement.classList.remove('dark')")
shot("narrow-light-partial")
print("Partial:", js("Array.from(document.querySelectorAll('[role=status]')).map(e=>e.textContent)"))
assert js("document.body.innerText.includes('Verified local skills only')")
print("PASS rendered keyboard selection, persisted identity, 44px touch removal, light/dark, narrow large text, loading/failure/partial and ordinary input")
