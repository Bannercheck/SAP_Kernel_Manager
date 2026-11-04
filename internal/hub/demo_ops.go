package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Bannercheck/SAP_Kernel_Manager/internal/disk"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/ops"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/discovery"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/kernel"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/sapcontrol"
	"github.com/Bannercheck/SAP_Kernel_Manager/internal/sap/status"
)

// agent simulates `kernelman <args>` on host; it returns the exit code.
func (r *DemoRemote) agent(ctx context.Context, h *demoHost, args []string, w io.Writer) int {
	flag := func(name string) string {
		for i, a := range args {
			if a == name && i+1 < len(args) {
				return args[i+1]
			}
		}
		return ""
	}
	if len(args) == 0 {
		return 2
	}
	if args[0] == "status" {
		r.mu.Lock()
		rep := r.report(h)
		r.mu.Unlock()
		_ = json.NewEncoder(w).Encode(rep)
		return 0
	}
	sid := flag("--sid")
	r.mu.Lock()
	sys := r.systems[sid]
	r.mu.Unlock()
	if sys == nil {
		fmt.Fprintf(w, "kernelman: no SAP system %s on %s\n", sid, h.name)
		return 1
	}
	say := func(format string, a ...any) bool {
		fmt.Fprintf(w, format+"\n", a...)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(r.Delay):
			return true
		}
	}
	mine := r.local(sys, h.name)
	say("  System %s · running as root → file operations and sapcontrol via su - %sadm", sid, strings.ToLower(sid))
	switch args[0] {
	case "backup":
		return r.demoBackup(sys, h, mine, say)
	case "stop":
		return r.demoStop(sys, h, mine, say)
	case "start":
		return r.demoStart(sys, h, mine, say)
	case "services":
		r.mu.Lock()
		for _, in := range mine {
			in.Srv = true
		}
		r.mu.Unlock()
		say("  SAP Host Agent   ⬤ RUNNING")
		say("  ✔ SAP Host Agent and every sapstartsrv are running · sapcontrol works again")
		return 0
	case "files", "update":
		return r.demoFilesUpdate(ctx, args[0], sys, h, mine, flag("--from"), say)
	case "rollback":
		dirs := r.dirs(sys, mine)
		n := 2*len(dirs) + 3
		for i, d := range dirs {
			say("  [%d/%d] Copy %s_%s over %s ... ok  cp -pR", i+1, n, d, time.Now().Format("20060102"), d)
		}
		r.mu.Lock()
		sys.Central = sys.Prev
		for _, in := range mine {
			in.Patch = sys.Prev
		}
		r.mu.Unlock()
		say("  [%d/%d] Read kernel version (disp+work -V) ... ok  793 Patch %d", n-1, n, sys.Prev)
		say("  [%d/%d] Verify every instance directory matches the central kernel ... ok", n, n)
		say("  ✔ Kernel %s restored: 793 Patch %d", sid, sys.Prev)
		return 0
	}
	fmt.Fprintf(w, "kernelman: unknown command %q\n", args[0])
	return 2
}

func (r *DemoRemote) local(sys *demoSys, host string) []*demoInst {
	var out []*demoInst
	for _, in := range sys.Insts {
		if in.Host == host {
			out = append(out, in)
		}
	}
	return out
}

func (r *DemoRemote) dirs(sys *demoSys, mine []*demoInst) []string {
	d := []string{"/sapmnt/" + sys.SID + "/exe/uc/linuxx86_64"}
	for _, in := range mine {
		d = append(d, "/usr/sap/"+sys.SID+"/"+in.Name+"/exe")
	}
	return d
}

func (r *DemoRemote) demoBackup(sys *demoSys, h *demoHost, mine []*demoInst, say func(string, ...any) bool) int {
	today := time.Now().Format("20060102")
	dirs := r.dirs(sys, mine)
	say("  Kernel directories → backups")
	for _, d := range dirs {
		say("    %s  →  %s_%s", d, d, today)
	}
	n, step := 2, 1
	for i := range dirs {
		if i == 0 && sys.BackupDay == today || i > 0 && sys.HostBackups[h.name] == today {
			n++
		} else {
			n += 3
		}
	}
	say("  [1/%d] Check free space ... ok  /sapmnt 41.2 GB free, a kernel needs 1.3 GB", n)
	files := 0
	for i, d := range dirs {
		cnt := 1204
		if i > 0 {
			cnt = 186
		}
		files += cnt
		if i == 0 && sys.BackupDay == today || i > 0 && sys.HostBackups[h.name] == today {
			step++
			say("  [%d/%d] Already backed up today: %s_%s ... ok  %d files · skipped", step, n, d, today, cnt)
			continue
		}
		say("  [%d/%d] Check %s ... ok  %d files, %s", step+1, n, d, cnt, humanBytes(int64(cnt)*930<<10))
		say("  [%d/%d] Copy to %s_%s ... ok  cp -pR (4s)", step+2, n, d, today)
		say("  [%d/%d] Verify %s_%s ... ok  %d files match", step+3, n, path.Base(d), today, cnt)
		step += 3
	}
	r.mu.Lock()
	sys.BackupDay, sys.HostBackups[h.name] = today, today
	r.mu.Unlock()
	say("  [%d/%d] Write listing /usr/sap/%s/.kernelman/backup_%s.log ... ok  %d directories", n, n, sys.SID, time.Now().Format("20060102_150405"), len(dirs))
	say("  ✔ Backup ready: %d directories, %d files", len(dirs), files)
	return 0
}

func (r *DemoRemote) demoStop(sys *demoSys, h *demoHost, mine []*demoInst, say func(string, ...any) bool) int {
	running := false
	for _, in := range sys.Insts {
		running = running || in.Running
	}
	say("  SAP Stop %s", sys.SID)
	n := 1 + 2*len(mine)
	if running {
		say("  [1/%d] StopSystem ALL ... ok  instance %s", n, mine[0].Nr)
	} else {
		say("  [1/%d] StopSystem ALL ... ok  no process running: system already down", n)
	}
	r.mu.Lock()
	for _, in := range sys.Insts {
		in.Running = false
	}
	r.mu.Unlock()
	for i, in := range mine {
		say("  [%d/%d] Wait until %s (%s) is stopped ... ok  all processes GRAY", i+2, n, in.Name, in.Nr)
	}
	for i, in := range mine {
		say("  [%d/%d] StopService %s (%s) ... ok  sapstartsrv stopped", len(mine)+i+2, n, in.Name, in.Nr)
		r.mu.Lock()
		in.Srv = false
		r.mu.Unlock()
	}
	say("  ✔ system %s stopped", sys.SID)
	return 0
}

func (r *DemoRemote) demoStart(sys *demoSys, h *demoHost, mine []*demoInst, say func(string, ...any) bool) int {
	say("  SAP Start %s", sys.SID)
	n := 1 + 2*len(mine)
	for i, in := range mine {
		if in.Patch != sys.Central {
			say("  ! /usr/sap/%s/%s/exe is not at the central kernel level (disp+work, sapstartsrv differ): run 6) Kernel Update so every directory is extracted", sys.SID, in.Name)
		}
		say("  [%d/%d] StartService %s (%s) ... ok  sapstartsrv up", i+1, n, in.Name, in.Nr)
		r.mu.Lock()
		in.Srv = true
		r.mu.Unlock()
	}
	for _, in := range sys.Insts {
		if in.Host != h.name && !in.Srv {
			say("  ! %s/%s: sapstartsrv not running there, StartSystem ALL cannot start that instance — on %s run KernelMan 5 → F", in.Host, in.Nr, in.Host)
		}
	}
	say("  [%d/%d] StartSystem ALL ... ok  instance %s", len(mine)+1, n, mine[0].Nr)
	r.mu.Lock()
	for _, in := range sys.Insts {
		if in.Srv {
			in.Running = true
		}
	}
	r.mu.Unlock()
	for i, in := range mine {
		if i == 0 {
			say("    … 30s: 2 process(es) not yet GREEN (disp+work YELLOW, igswd_mt GRAY), waiting up to 15m0s")
		}
		say("  [%d/%d] Wait until %s (%s) is running ... ok  all processes GREEN", len(mine)+i+2, n, in.Name, in.Nr)
	}
	say("  ✔ system %s started", sys.SID)
	return 0
}

func (r *DemoRemote) demoFilesUpdate(ctx context.Context, op string, sys *demoSys, h *demoHost, mine []*demoInst, from string, say func(string, ...any) bool) int {
	r.mu.Lock()
	var names []string
	for p, f := range r.files[h.name] {
		if (from == "" || path.Dir(p) == from) && time.Since(f.mtime) < 24*time.Hour && strings.EqualFold(path.Ext(p), ".sar") {
			names = append(names, path.Base(p))
		}
	}
	r.mu.Unlock()
	var sars []ops.SARFile
	for _, n := range names {
		if f, ok := ops.ParseSARName(n); ok {
			sars = append(sars, f)
		}
	}
	ops.SortForApply(sars)
	where := "the whole server (/) including every user's home directory"
	if from != "" {
		where = from
	}
	say("  scanning %s for .SAR files placed today (%s); system directories, kernel directories and backups are skipped", where, time.Now().Format("2006-01-02"))
	if len(sars) == 0 {
		say("  ✘ no .SAR files placed today (%s) under %s", time.Now().Format("2006-01-02"), where)
		return 1
	}
	say("  Archives placed on this server today, in apply order (lowest patch first)")
	for i, f := range sars {
		say("    %d  %-28s %-9s %d", i+1, f.Name, f.Label, f.Patch)
	}
	target := ops.TargetPatch(sars)
	say("    → target level after apply: patch %d", target)
	dirs := r.dirs(sys, mine)
	if op == "files" {
		n := 2*len(dirs) + 2
		say("  [1/%d] Check permissions of %d archive(s) in %s ... ok  %d already 0755", n, len(sars), where, len(sars))
		for i, d := range dirs {
			say("  [%d/%d] Copy %d archive(s) to %s ... ok  cp -p", 2*i+2, n, len(sars), d)
			say("  [%d/%d] chown -R %sadm:sapsys %s ... ok  done", 2*i+3, n, strings.ToLower(sys.SID), d)
		}
		say("  [%d/%d] List archives in %s ... ok  ls -la", n, n, dirs[0])
		say("  ✔ %d archive(s) copied into %d kernel directories · chown done", len(sars), len(dirs))
		return 0
	}
	if day := time.Now().Format("20060102"); sys.HostBackups[h.name] == day {
		say("  ✔ backup from today: %s_%s", dirs[0], day)
	} else {
		say("  ! no kernel backup from today.")
		if r.demoBackup(sys, h, mine, say) != 0 {
			return 1
		}
	}
	say("  SAPCAR: /sapmnt/%s/exe/uc/linuxx86_64/SAPCAR", sys.SID)
	say("  current kernel: 793 Patch %d", sys.Central)
	n, step := len(dirs)*len(sars)+len(dirs)+3, 0
	for _, d := range dirs {
		for _, f := range sars {
			step++
			cnt := 6
			if f.Full {
				cnt = 1180
			}
			if !say("  [%d/%d] SAPCAR -xvf %s  (%s %d) in %s ... ok  %d files", step, n, f.Name, f.Label, f.Patch, d, cnt) {
				return 130
			}
		}
	}
	for _, d := range dirs {
		step++
		say("  [%d/%d] chown -R %sadm:sapsys %s ... ok  done", step, n, strings.ToLower(sys.SID), d)
	}
	r.mu.Lock()
	if sys.Central != target {
		sys.Prev = sys.Central
	}
	sys.Central = target
	for _, in := range mine {
		in.Patch = target
	}
	r.mu.Unlock()
	say("  [%d/%d] saproot.sh %s ... ok  done", step+1, n, sys.SID)
	say("  [%d/%d] Read kernel version (disp+work -V) ... ok  793 Patch %d", step+2, n, target)
	var ok []string
	for _, in := range mine {
		ok = append(ok, in.Name+"/exe")
	}
	say("  [%d/%d] Verify every instance directory matches the central kernel ... ok  %s = central", n, n, strings.Join(ok, ", "))
	say("  ✔ Kernel %s: 793 Patch %d → 793 Patch %d", sys.SID, sys.Prev, target)
	return 0
}

// report is the status JSON the real agent prints for host h; r.mu is held.
func (r *DemoRemote) report(h *demoHost) status.Report {
	rep := status.Report{GeneratedAt: time.Now(),
		Host: status.HostInfo{Hostname: h.name, OS: "linux", OSVersion: h.release, Arch: "amd64", KernelDirName: "linuxx86_64", User: "root", Privileged: true},
		HostAgent: discovery.HostAgent{Installed: true, Running: true, Path: "/usr/sap/hostctrl/exe/saphostexec", Version: kernel.Version{Release: 722, Patch: 65},
			Processes: []string{"saphostexec running (pid = 4242)", "sapstartsrv running (pid = 4243)", "saposcol running (pid = 4244)"}},
		Disk: status.Disk{Roots: []string{"/usr/sap", "/sapmnt"}, Filesystems: []disk.Filesystem{
			{Mount: "/usr/sap", Path: "/usr/sap", SizeKB: 100 << 20, UsedKB: int64(h.usrsapPct) << 20, AvailKB: int64(100-h.usrsapPct) << 20, UsePct: h.usrsapPct},
			{Mount: "/sapmnt", Path: "/sapmnt", SizeKB: 50 << 20, UsedKB: int64(h.sapmntPct) << 19, AvailKB: int64(100-h.sapmntPct) << 19, UsePct: h.sapmntPct}}},
	}
	var sids []string
	for sid, s := range r.systems {
		if len(r.local(s, h.name)) > 0 {
			sids = append(sids, sid)
		}
	}
	sort.Strings(sids)
	for _, sid := range sids {
		s := r.systems[sid]
		dir := "/sapmnt/" + sid + "/exe/uc/linuxx86_64"
		sys := status.System{SID: sid, Type: s.Type, Database: s.DB, Kernel: kernel.Version{Release: 793, Patch: s.Central, Platform: "linuxx86_64"},
			KernelSource: "disp+work -V in DIR_CT_RUN", DirCtRun: dir, GlobalKernelDir: dir, DirExeRoot: "/usr/sap/" + sid + "/SYS/exe"}
		var sts []string
		for _, in := range s.Insts {
			si := status.Instance{Nr: in.Nr, Name: in.Name, Type: in.Type, TypeDesc: discovery.TypeDescription(in.Type), Host: in.Host, Status: "GRAY"}
			if in.Running {
				si.Status = "GREEN"
			}
			if in.Host == h.name {
				si.Local, si.Profile, si.DirExecutable = true, fmt.Sprintf("/usr/sap/%s/SYS/profile/%s_%s_%s", sid, sid, in.Name, in.Host), "/usr/sap/"+sid+"/"+in.Name+"/exe"
				si.Sapstartsrv, si.ExeChecked = "running", 6
				if !in.Srv {
					si.Sapstartsrv, si.Status = "not running", "GRAY"
				}
				if in.Patch != s.Central {
					si.ExeDiffers = []string{"sapstartsrv", "disp+work", "gwrd", "icman"}
				}
			} else {
				si.Sapstartsrv = "remote"
			}
			sts = append(sts, si.Status)
			sys.Instances = append(sys.Instances, si)
		}
		sys.Status = sapcontrol.Aggregate(sts)
		rep.Systems = append(rep.Systems, sys)
	}
	return rep
}
