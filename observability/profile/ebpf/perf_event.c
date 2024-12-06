//go:build ignore

#include "vmlinux.h"
#include "bpf_map.h"
#include "bpf_core_read.h"
#include "bpf_tracing.h"

char __license[] SEC("license") = "Dual MIT/GPL";

#define USER_STACKID_FLAGS (0 | BPF_F_FAST_STACK_CMP | BPF_F_USER_STACK)

#define PERF_MAX_STACK_DEPTH      127
#define PROFILE_MAPS_SIZE         16384

struct stack_key {
	__u32 pid;
	__s64 stack_id;
	char  comm[16];
};

struct arguments
{
  __u32 pid;
};

struct arguments *unused __attribute__((unused));
struct stack_key *unused1 __attribute__((unused));


struct
{
  __uint(type, BPF_MAP_TYPE_ARRAY);
  __uint(max_entries, 1);
  __type(key, __u32);
  __type(value, struct arguments);
} params_array SEC(".maps");

struct
{
  __uint(type, BPF_MAP_TYPE_STACK_TRACE);
	__uint(key_size, sizeof(u32));
  __uint(value_size, PERF_MAX_STACK_DEPTH * sizeof(u64));
  __uint(max_entries, PROFILE_MAPS_SIZE);
} stacks SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, struct stack_key);
	__type(value, u32);
	__uint(max_entries, PROFILE_MAPS_SIZE);
} counts SEC(".maps");

static unsigned int pid_namespace(struct task_struct *task)
{
  struct pid *pid;
  unsigned int level;
  struct upid upid;
  unsigned int inum;

  /*  get the pid namespace by following task_active_pid_ns(),
 *  pid->numbers[pid->level].ns
 */
  pid = BPF_CORE_READ(task, thread_pid);
  level = BPF_CORE_READ(pid, level);
  bpf_core_read(&upid, sizeof(upid), &pid->numbers[level]);
  inum = BPF_CORE_READ(upid.ns, ns.inum);

  return inum;
}

SEC("perf_event")
int do_perf_event(struct bpf_perf_event_data *ctx)
{
  /*
  struct bpf_pidns_info pidns;
  u64 id = bpf_get_ns_current_pid_tgid(4, 4026533488, &pidns, sizeof(struct bpf_pidns_info));
  u32 tgid = id >> 32;
  u32 pid = id;

  struct task_struct *task = (struct task_struct *)bpf_get_current_task();
  
  struct arguments *args = 0;
  __u32 argsKey = 0;
  args = (struct arguments *)bpf_map_lookup_elem(&params_array, &argsKey);
  if (!args)
  {
    bpf_printk("no args");
    return -1;
  }

  u64 cg = bpf_get_current_cgroup_id();

  if (pidns.pid != 1) {
    return 0;
  }

  bpf_printk("got event for pid %d /%d (cgroup %d)", pidns.pid, id, cg);

  */

  u64 id = bpf_get_current_pid_tgid();
  u32 tgid = id >> 32;
  u32 pid = id;

  bpf_printk("got event");

  struct stack_key key;
  key.pid = 1;
  bpf_get_current_comm(&key.comm, sizeof(key.comm));
  key.stack_id = bpf_get_stackid(ctx, &stacks, USER_STACKID_FLAGS);
  
  
  u32* val = bpf_map_lookup_elem(&counts, &key);
  if (val)
    (*val)++;
  else {
    u32 one = 1;
    bpf_map_update_elem(&counts, &key, &one, BPF_NOEXIST);
  }

  return 0;
}

typedef struct stacktrace_event{
	u64 ip;
	u32 pid;
	u32 cpu_id;
	u32 tgid;
	char comm[TASK_COMM_LEN];
	s32 ustack_sz;
	u64 ustack[128];
	s32 kstack_sz;
	u64 kstack[128];
} Trace;

struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 256 * 1024);
} events SEC(".maps");

SEC("perf_event")
int profile(void *ctx)
{
	int pid = bpf_get_current_pid_tgid() >> 32;
	int cpu_id = bpf_get_smp_processor_id();
	Trace *event;
	int cp;

  bpf_printk("profile event: reserve\n");
	event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
	if (!event) {
  bpf_printk("profile event: error\n");
		return 1;
  }

	event->pid = pid;
	event->cpu_id = cpu_id;

	if (bpf_get_current_comm(event->comm, sizeof(event->comm)))
		event->comm[0] = 0;

	event->kstack_sz = bpf_get_stack(ctx, event->kstack, sizeof(event->kstack), 0);

	event->ustack_sz = bpf_get_stack(ctx, event->ustack, sizeof(event->ustack), BPF_F_USER_STACK);

	bpf_ringbuf_submit(event, 0);

  bpf_printk("profile event: done\n");
	return 0;
}
