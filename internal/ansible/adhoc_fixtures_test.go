package ansible

// Captured from ansible-core 2.21.4 running `ansible <alias> -i <generated inventory> -m <module>` against a local
// connection target (real output, only the warning line is representative).

const adhocPingOutput = `[WARNING]: Host 'web-1' is using the discovered Python interpreter at '/usr/bin/python3.14', but future installation of another Python interpreter could cause a different interpreter to be discovered. See https://docs.ansible.com/ansible-core/2.21/reference_appendices/interpreter_discovery.html for more information.
web-1 | SUCCESS => {
    "ansible_facts": {
        "discovered_interpreter_python": "/usr/bin/python3.14"
    },
    "changed": false,
    "ping": "pong"
}
`

const adhocShellOutput = `[WARNING]: Host 'web-1' is using the discovered Python interpreter at '/usr/bin/python3.14', but future installation of another Python interpreter could cause a different interpreter to be discovered. See https://docs.ansible.com/ansible-core/2.21/reference_appendices/interpreter_discovery.html for more information.
web-1 | CHANGED | rc=0 >>
hellooops
`

const adhocFailedOutput = `[WARNING]: Host 'web-1' is using the discovered Python interpreter at '/usr/bin/python3.14', but future installation of another Python interpreter could cause a different interpreter to be discovered. See https://docs.ansible.com/ansible-core/2.21/reference_appendices/interpreter_discovery.html for more information.
[ERROR]: Task failed: Module failed: The command exited with a non-zero return code.
Origin: <adhoc 'command' task>

{'action': 'command', 'args': {'_raw_params': 'false'}, 'timeout': 0, 'async_val': 0, 'poll': 15}

web-1 | FAILED | rc=1 >>
The command exited with a non-zero return code.
`

const adhocUnreachableOutput = `[ERROR]: Task failed: Failed to connect to the host via ssh: ssh: connect to host 127.0.0.1 port 1: Connection refused
Origin: <adhoc 'ping' task>

{'action': 'ping', 'args': {}, 'timeout': 0, 'async_val': 0, 'poll': 15}

web-1 | UNREACHABLE! => {
    "changed": false,
    "msg": "Task failed: Failed to connect to the host via ssh: ssh: connect to host 127.0.0.1 port 1: Connection refused",
    "unreachable": true
}
`

const adhocColoredOutput = `[0;32mweb-1 | SUCCESS | rc=0 >>[0m
[0;33mweb-2 | CHANGED | rc=0 >>[0m
hello from web-2
[0;31mweb-3 | FAILED | rc=2 >>[0m
boom
[0;31mweb-4 | UNREACHABLE! => {[0m
    "changed": false,
    "msg": "Failed to connect to the host via ssh",
    "unreachable": true
}
`
