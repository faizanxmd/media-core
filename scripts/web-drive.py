#!/usr/bin/env python3
"""Drive the site in headless Firefox on mint, to check the web player and Manager after a change.
Usage: scripts/web-drive.py steps.json   (Firefox only; it speaks Marionette itself, nothing to install)
Always run ["nowatch"] before opening a title: without it a test rewrites the watch progress of what it plays.
script: [["go", url], ["wait", seconds], ["js", code] -> prints result, ["shot", file], ["size", w, h], ["key", key]]"""
import socket, json, sys, time, base64, subprocess, os, shutil, tempfile
prof = tempfile.mkdtemp(prefix="ffp")
open(os.path.join(prof, "user.js"), "w").write('user_pref("media.autoplay.default", 0);user_pref("media.autoplay.blocking_policy", 0);user_pref("marionette.port", 2828);user_pref("media.hardware-video-decoding.enabled", false);')
ff = subprocess.Popen(["firefox", "--headless", "--no-remote", "--marionette", "--profile", prof, "--window-size", "1440,900"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
try:
    s = None
    for _ in range(60):
        try: s = socket.create_connection(("127.0.0.1", 2828), timeout=60); break
        except OSError: time.sleep(0.5)
    def recv():
        buf = b""
        while b":" not in buf: buf += s.recv(1)
        n = int(buf[:-1]); data = b""
        while len(data) < n: data += s.recv(n - len(data))
        return json.loads(data)
    recv()
    mid = [0]
    def cmd(name, **params):
        mid[0] += 1
        p = json.dumps([0, mid[0], name, params]).encode()
        s.sendall(str(len(p)).encode() + b":" + p)
        while True:
            r = recv()
            if r[0] == 1 and r[1] == mid[0]:
                if r[2]: raise RuntimeError(r[2])
                return r[3]
    cmd("WebDriver:NewSession", capabilities={"alwaysMatch": {"acceptInsecureCerts": True}})
    for step in json.load(open(sys.argv[1])):
        op = step[0]
        try:
            if op == "go": cmd("WebDriver:Navigate", url=step[1])
            elif op == "wait": time.sleep(step[1])
            elif op == "size": cmd("WebDriver:SetWindowRect", width=step[1], height=step[2])
            elif op == "js":
                r = cmd("WebDriver:ExecuteScript", script=step[1], args=[])
                print("js>", json.dumps(r.get("value"))[:1500])
            elif op == "nowatch":  # tests must not touch the user's watch progress
                cmd("WebDriver:ExecuteScript", script="const f=window.fetch;window.fetch=(u,o)=>String(u).includes('/api/v1/progress')||String(u).includes('/api/v1/watched')?Promise.resolve(new Response('{}')):f(u,o);", args=[])
            elif op == "key":
                cmd("WebDriver:ExecuteScript", script="const k=arguments[0];for(const t of ['keydown','keyup'])window.dispatchEvent(new KeyboardEvent(t,{key:k,bubbles:true,cancelable:true}));", args=[step[1]])
            elif op == "shot":
                r = cmd("WebDriver:TakeScreenshot", full=False)
                open(step[1], "wb").write(base64.b64decode(r["value"])); print("shot", step[1])
        except Exception as e:
            print("ERR", op, str(e)[:300])
finally:
    ff.terminate()
    try: ff.wait(10)
    except Exception: ff.kill()
    shutil.rmtree(prof, ignore_errors=True)
