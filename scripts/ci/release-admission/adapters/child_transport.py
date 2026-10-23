"""Future runtime only: one exact finite Node client, no shell, no retries."""

from . import adapter_context
import os
import selectors
import signal
import subprocess
import time

from . import driver_data as d


def run(argv,*,stop,before_spawn,env):
    d.require(env==d.ENV and type(argv) is list and len(argv)==12,'exact-frozen-child-environment-and-argv')
    d.require(argv[0]==str(adapter_context.get().node_path)
        and argv[1]==str(adapter_context.get().transport_root/'client.cjs')
        and argv[2::2]==['--owner-id','--runtime-dir','--source-sha256','--review-sha256','--request-json'],
        'only-fixed-reviewed-Node-client-argv')
    closing=False
    def cancelled(signum,frame):
        if not closing:raise RuntimeError('paired:owned-child-cancelled')
    previous=signal.getsignal(signal.SIGTERM)
    process=None;selector=None;raw=bytearray()
    try:
        signal.signal(signal.SIGTERM,cancelled)
        selector=selectors.DefaultSelector()
        before_spawn()  # Last callback before the only launch attempt.
        process=subprocess.Popen(argv,cwd=str(adapter_context.get().transport_root),
            env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,
            close_fds=True,start_new_session=True,shell=False)
        os.set_blocking(process.stdout.fileno(),False);selector.register(process.stdout,selectors.EVENT_READ)
        eof=False
        while not eof:
            remaining=stop-time.monotonic();d.require(remaining>0,'finite-original-child-closure-window')
            for key,_ in selector.select(min(remaining,0.05)):
                chunk=os.read(key.fd,65536)
                if not chunk:eof=True;break
                raw.extend(chunk);d.require(len(raw)<=d.LIMIT,'whole-child-stdout-bound')
        remaining=stop-time.monotonic();d.require(remaining>0,'finite-child-exit-window')
        code=process.wait(timeout=remaining)
        return {'stdout':bytes(raw),'exitCode':code,'closed':True}
    except subprocess.TimeoutExpired:
        raise ValueError('paired:child-exit-deadline') from None
    finally:
        closing=True
        try:
            if selector is not None:selector.close()
            if process is not None:
                try:
                    if process.poll() is None:
                        try:os.killpg(process.pid,signal.SIGKILL)
                        except ProcessLookupError:pass
                        # Reap only this owned child, using the pinned R2 receipt
                        # cleanup bound. This grants no further execution window.
                        try:process.wait(timeout=2)
                        except subprocess.TimeoutExpired:raise ValueError('paired:owned-child-reap-unverified') from None
                finally:
                    if process.stdout is not None:process.stdout.close()
        finally:signal.signal(signal.SIGTERM,previous)
