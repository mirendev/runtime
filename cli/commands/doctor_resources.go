package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/api/disk/disk_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/storage/storage_v1alpha"
	"miren.dev/runtime/pkg/ui"
)

type doctorResources struct {
	disks   []*disk_v1alpha.ResourceHealth
	volumes []*disk_v1alpha.ResourceHealth
}

func gatherDoctorResources(ctx *Context) (*doctorResources, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cl, err := ctx.rpcClient(probeCtx, "dev.miren.runtime/disk-health")
	if err == nil {
		defer cl.Close()
		err = requireRPCMethod(probeCtx, cl, "list")
	}
	if isMissingRPC(err) {
		return gatherDoctorResourcesLegacy(ctx, probeCtx)
	}
	if err != nil {
		return nil, err
	}
	health := disk_v1alpha.NewDiskHealthClient(cl)
	r := &doctorResources{}
	for _, kindName := range []string{"disk", "disk_volume"} {
		for cursor := ""; ; {
			page, err := health.List(probeCtx, kindName, cursor, 200)
			if err != nil {
				return nil, fmt.Errorf("list %s: %w", kindName, err)
			}
			switch kindName {
			case "disk":
				r.disks = append(r.disks, page.Resources()...)
			case "disk_volume":
				r.volumes = append(r.volumes, page.Resources()...)
			}
			cursor = page.Cursor()
			if cursor == "" {
				break
			}
		}
	}
	if err := probeCtx.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

func gatherDoctorResourcesLegacy(ctx *Context, probeCtx context.Context) (*doctorResources, error) {
	cl, err := ctx.rpcClient(probeCtx, "entities")
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	eac := entityserver_v1alpha.NewEntityAccessClient(cl)
	r := &doctorResources{}
	for _, kindName := range []string{"disk", "disk_volume"} {
		kind, err := eac.LookupKind(probeCtx, kindName)
		if err != nil {
			return nil, fmt.Errorf("lookup %s: %w", kindName, err)
		}
		for cursor := ""; ; {
			page, err := eac.ListPage(probeCtx, kind.Attr(), cursor, 200)
			if err != nil {
				return nil, fmt.Errorf("list %s: %w", kindName, err)
			}
			if page.Incomplete() {
				return nil, fmt.Errorf("resource health scan unavailable: incomplete %s scan", kindName)
			}
			for _, entry := range page.Values() {
				info := &disk_v1alpha.ResourceHealth{}
				if kindName == "disk" {
					var disk storage_v1alpha.Disk
					disk.Decode(entry.Entity())
					info.SetId(string(disk.ID))
					info.SetState(string(disk.Status))
					r.disks = append(r.disks, info)
				} else {
					var volume storage_v1alpha.DiskVolume
					volume.Decode(entry.Entity())
					info.SetId(string(volume.ID))
					info.SetState(string(volume.ActualState))
					if volume.ActualState == storage_v1alpha.DV_ERROR {
						info.SetDiagnostic("volume lifecycle failed; inspect server logs")
					}
					r.volumes = append(r.volumes, info)
				}
			}
			cursor = page.Cursor()
			if cursor == "" {
				break
			}
		}
	}
	if err := probeCtx.Err(); err != nil {
		return nil, err
	}
	return r, nil
}

func gatherDoctorApps(ctx *Context) ([]*app_v1alpha.AppInfo, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cl, err := ctx.rpcClient(probeCtx, "dev.miren.runtime/app")
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	res, err := app_v1alpha.NewCrudClient(cl).List(probeCtx)
	if err != nil {
		return nil, err
	}
	return res.Apps(), nil
}

func resourceUnavailable(env *doctorEnv) (checkResult, bool) {
	if !env.configured() {
		return checkResult{Status: checkSkip, Summary: "(no cluster configured)"}, true
	}
	if env.connErr != nil {
		return checkResult{Status: checkSkip, Summary: "(server unreachable)"}, true
	}
	if env.resourcesErr != nil || env.resources == nil {
		return checkResult{Status: checkSkip, Summary: fmt.Sprintf("(resource scan unavailable: %v)", env.resourcesErr)}, true
	}
	return checkResult{}, false
}

func checkApps(env *doctorEnv) checkResult {
	if !env.configured() {
		return checkResult{Status: checkSkip, Summary: "(no cluster configured)"}
	}
	if env.connErr != nil {
		return checkResult{Status: checkSkip, Summary: "(server unreachable)"}
	}
	if env.appsErr != nil {
		return checkResult{Status: checkSkip, Summary: fmt.Sprintf("(app health unavailable: %v)", env.appsErr)}
	}
	var failing []string
	for _, app := range env.apps {
		if app.Health() == "crashed" {
			failing = append(failing, fmt.Sprintf("%s (%d crashes, retry in %s)", app.Name(), app.CrashCount(), formatDuration(time.Duration(app.CooldownSeconds())*time.Second)))
		}
	}
	if len(failing) == 0 {
		return checkResult{Status: checkOK, Summary: "no apps in crash cooldown"}
	}
	sort.Strings(failing)
	return checkResult{Status: checkFail, Summary: strings.Join(failing, ", "), Problem: &ui.Diagnostic{
		Summary: "apps are crash-looping", Detail: strings.Join(failing, ", "),
		Actions: []ui.Action{{Command: "miren app status -a <app>", Note: "inspect service failures and exit codes"}, {Command: "miren sandbox list --all", Note: "inspect dead sandboxes"}},
	}}
}

func checkDisks(env *doctorEnv) checkResult {
	if skip, unavailable := resourceUnavailable(env); unavailable {
		return skip
	}
	var failing []string
	for _, disk := range env.resources.disks {
		if disk.State() == "status.error" {
			failing = append(failing, fmt.Sprintf("disk %s", disk.Id()))
		}
	}
	for _, volume := range env.resources.volumes {
		if volume.State() == "actual_state.dv_error" {
			failing = append(failing, fmt.Sprintf("volume %s: %s", volume.Id(), volume.Diagnostic()))
		}
	}
	if len(failing) == 0 {
		return checkResult{Status: checkOK, Summary: "no disk or volume errors"}
	}
	sort.Strings(failing)
	return checkResult{Status: checkFail, Summary: strings.Join(failing, ", "), Problem: &ui.Diagnostic{
		Summary: "disk or volume errors detected", Detail: strings.Join(failing, ", "),
		Actions: []ui.Action{{Command: "miren debug disk list", Note: "inspect disk states"}},
	}}
}
