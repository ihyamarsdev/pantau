package ansible

import (
	"strings"
	"testing"
)

const outlineSample = `- name: Deploy app   # the play
  hosts: web
  become: true
  vars:
    app_port: 8080
  pre_tasks:
    - name: Wait for ssh
      ansible.builtin.wait_for_connection:
  roles:
    - common
    - role: ansistrano.deploy
      vars:
        ansistrano_deploy_to: /srv/app
      tags: [deploy]
  tasks:
    - name: Show   # keep this comment
      debug:
        msg: "{{ app_port }}"
  post_tasks:
    - debug: msg=done
  handlers:
    - name: restart app
      debug: msg=restarted
`

func TestSplitJoinPlaybook(t *testing.T) {
	o := SplitPlaybook(outlineSample)
	if !o.Structured {
		t.Fatalf("not structured: %s", o.Reason)
	}
	if !strings.Contains(o.Play, "hosts: web") || strings.Contains(o.Play, "post_tasks:") || strings.Contains(o.Play, "handlers:") || strings.Contains(o.Play, "roles:") || strings.Contains(o.Play, "tasks:\n    - name: Show") {
		t.Fatalf("play part wrong:\n%s", o.Play)
	}
	if !strings.Contains(o.PostTasks, "msg=done") || !strings.Contains(o.Handlers, "restart app") {
		t.Fatalf("post_tasks/handlers parts wrong: %q / %q", o.PostTasks, o.Handlers)
	}
	if !strings.Contains(o.PreTasks, "wait_for_connection") || !strings.Contains(o.Tasks, "# keep this comment") {
		t.Fatalf("task parts wrong: %q / %q", o.PreTasks, o.Tasks)
	}
	if len(o.Roles) != 2 || o.Roles[0] != (OutlineRole{Name: "common"}) || o.Roles[1].Name != "ansistrano.deploy" || !strings.Contains(o.Roles[1].Params, "ansistrano_deploy_to") || !strings.Contains(o.Roles[1].Params, "tags:") {
		t.Fatalf("roles wrong: %+v", o.Roles)
	}

	back, err := JoinPlaybook(o)
	if err != nil {
		t.Fatal(err)
	}
	again := SplitPlaybook(back)
	if !again.Structured || again.Play != o.Play || again.PreTasks != o.PreTasks || again.Tasks != o.Tasks || again.PostTasks != o.PostTasks || again.Handlers != o.Handlers || len(again.Roles) != 2 || again.Roles[1] != o.Roles[1] {
		t.Fatalf("join/split is not stable:\n%s", back)
	}
	if strings.Index(back, "pre_tasks:") > strings.Index(back, "roles:") || strings.Index(back, "roles:") > strings.Index(back, "  tasks:") || strings.Index(back, "  tasks:") > strings.Index(back, "post_tasks:") || strings.Index(back, "post_tasks:") > strings.Index(back, "handlers:") {
		t.Fatalf("keys are not in run order:\n%s", back)
	}
	if !strings.Contains(back, "# keep this comment") {
		t.Fatalf("comment lost:\n%s", back)
	}
}

func TestJoinPlaybookValidation(t *testing.T) {
	cases := map[string]Outline{
		"own key in play":    {Play: "hosts: all\ntasks: []"},
		"post_tasks in play": {Play: "hosts: all\npost_tasks: []"},
		"handlers not list":  {Play: "hosts: all", Handlers: "name: x"},
		"tasks not list":     {Play: "hosts: all", Tasks: "debug: msg=x"},
		"bad role params":    {Play: "hosts: all", Roles: []OutlineRole{{Name: "r", Params: "- a"}}},
		"role name param":    {Play: "hosts: all", Roles: []OutlineRole{{Name: "r", Params: "name: x"}}},
		"empty role name":    {Play: "hosts: all", Roles: []OutlineRole{{Name: " "}}},
		"empty play":         {},
		"broken yaml":        {Play: "hosts: [all"},
	}
	for name, o := range cases {
		if _, err := JoinPlaybook(o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSplitPlaybookRejects(t *testing.T) {
	for name, c := range map[string]string{
		"two plays": "- hosts: a\n  tasks: []\n- hosts: b\n  tasks: []\n",
		"empty":     "",
		"not list":  "hosts: all\n",
		"broken":    "- hosts: [all\n",
	} {
		if o := SplitPlaybook(c); o.Structured || o.Reason == "" {
			t.Errorf("%s: should not be structured: %+v", name, o)
		}
	}
	if o := SplitPlaybook("- hosts: all\n  roles: [a, b]\n"); !o.Structured || len(o.Roles) != 2 {
		t.Errorf("flow-style roles: %+v", o)
	}
}
