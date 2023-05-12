#include <asm/uaccess.h>
#include <linux/hashtable.h>
#include <linux/init.h>
#include <linux/ip.h>
#include <linux/kernel.h>
#include <linux/module.h>
#include <linux/mutex.h>
#include <linux/netfilter.h>
#include <linux/netfilter_ipv4.h>
#include <linux/poll.h>
#include <linux/proc_fs.h>
#include <linux/string.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/wait.h>

#define MIN_PORT 10000
#define MAX_PORT 11000
#define MAX_PORT_NUM (MAX_PORT - MIN_PORT + 1)

#define FIGGER_MODULE "FIGGER "

#define PROC_DIR "figger"
struct proc_dir_entry *dir_entry;

#define BUFFER_SIZE 5

struct port_t {
    int state;  // 0 -> stopped; 1 -> starting; 2 -> started; 3 -> stopping
    int last_read_state;
    wait_queue_head_t wq_head;
    struct mutex poll_mutex;
};

struct port_t *port_list;

int check_port_range(int port_num) {
    return port_num >= MIN_PORT && port_num <= MAX_PORT;
}

void change_port_state(struct port_t *port, int state) {
    mutex_lock(&port->poll_mutex);
    port->state = state;
    if (port->state == 1 || port->state == 3) {
        printk(KERN_INFO FIGGER_MODULE "wake up, state: %d\n", port->state);
        wake_up_interruptible(&port->wq_head);
    }
    mutex_unlock(&port->poll_mutex);
}

struct port_t *get_port_by_port_num(int port_num, int is_udp) {
    int index;
    index = port_num - MIN_PORT;
    if (is_udp) {
        index += MAX_PORT_NUM;
    }
    return &port_list[index];
}

struct port_t *get_port(struct file *file) {
    char *filename;
    int len;
    int i;
    int j;
    int port_num;
    filename = file->f_path.dentry->d_name.name;
    len = strlen(filename);

    i = len - 1;
    port_num = 0;
    while (i >= 0 && filename[i] >= '0' && filename[i] <= '9') {
        i--;
    }
    j = i + 1;
    while (j < len) {
        port_num = port_num * 10 + filename[j] - '0';
        j++;
    }

    return get_port_by_port_num(port_num, filename[i - 3] == 'u');
}

static ssize_t proc_read(struct file *file, char __user *usr_buf, size_t count,
                         loff_t *pos) {
    if (*pos > 0 || count < BUFFER_SIZE) return 0;
    struct port_t *port;
    port = get_port(file);
    int rv = 0;
    char buffer[BUFFER_SIZE];
    rv = sprintf(buffer, "%c", '0' + port->state);
    copy_to_user(usr_buf, buffer, rv);
    *pos = rv;

    port->last_read_state = port->state;

    return rv;
}

static ssize_t proc_write(struct file *file, const char __user *usr_buf,
                          size_t count, loff_t *pos) {
    struct port_t *port;
    port = get_port(file);

    char buffer[BUFFER_SIZE];
    copy_from_user(buffer, usr_buf, count);
    change_port_state(port, buffer[0] - '0');

    return count;
}

static __poll_t proc_poll(struct file *file, struct poll_table_struct *wait) {
    unsigned int mask;

    struct port_t *port;
    port = get_port(file);

    mutex_lock(&port->poll_mutex);
    poll_wait(file, &port->wq_head, wait);
    if (port->last_read_state != port->state) {
        mask |= POLLIN | POLLRDNORM;
    }

    mutex_unlock(&port->poll_mutex);

    return mask;
}

static struct proc_ops proc_ops = {
    .proc_read = proc_read,
    .proc_write = proc_write,
    .proc_poll = proc_poll,
};

static int deal_with_tcp_udp(int port_num) {
    if (!check_port_range(port_num)) return NF_ACCEPT;

    struct port_t *port = get_port_by_port_num(port_num, 0);
    switch (port->state) {
        case 0:
            change_port_state(port, 1);
            return NF_DROP;

        case 1:
            return NF_DROP;

        case 2:
            return NF_ACCEPT;

        case 3:
            change_port_state(port, 2);
            return NF_ACCEPT;
    }
    return NF_ACCEPT;
}

static int nf_handler(void *priv, struct sk_buff *skb,
                      const struct nf_hook_state *state) {
    if (!skb) return NF_ACCEPT;

    struct iphdr *ip_header = (struct iphdr *)skb_network_header(skb);
    if (ip_header->protocol == IPPROTO_TCP) {
        struct tcphdr *tcp_header = (struct tcphdr *)skb_transport_header(skb);
        int port_num = ntohs(tcp_header->dest);
        return deal_with_tcp_udp(port_num);
    } else if (ip_header->protocol == IPPROTO_UDP) {
        struct udphdr *udp_header = (struct udphdr *)skb_transport_header(skb);
        int port_num = ntohs(udp_header->dest);
        return deal_with_tcp_udp(port_num);
    }

    return NF_ACCEPT;
}

static struct nf_hook_ops *nf_ops = NULL;

void register_nf_hook(void) {
    nf_ops = (struct nf_hook_ops *)kcalloc(1, sizeof(struct nf_hook_ops),
                                           GFP_KERNEL);
    if (nf_ops != NULL) {
        nf_ops->hook = (nf_hookfn *)nf_handler;
        nf_ops->hooknum = NF_INET_PRE_ROUTING;
        nf_ops->pf = NFPROTO_IPV4;
        nf_ops->priority = NF_IP_PRI_FIRST;

        nf_register_net_hook(&init_net, nf_ops);
    }
}

void rm_nf_hook(void) {
    if (nf_ops != NULL) {
        nf_unregister_net_hook(&init_net, nf_ops);
        kfree(nf_ops);
    }
}

void create_batch_proc(void) {
    // create proc dir
    dir_entry = proc_mkdir(PROC_DIR, NULL);

    int i;
    char *name;
    name = (char *)kmalloc(10, GFP_KERNEL);
    for (i = MIN_PORT; i <= MAX_PORT; i++) {
        sprintf(name, "tcp-%d", i);
        proc_create(name, 0, dir_entry, &proc_ops);
        printk(KERN_INFO FIGGER_MODULE "/proc/%s created\n", name);

        sprintf(name, "udp-%d", i);
        proc_create(name, 0, dir_entry, &proc_ops);
        printk(KERN_INFO FIGGER_MODULE "/proc/%s created\n", name);
    }
}

void remove_batch_proc(void) {
    int i;
    char *name;
    name = (char *)kmalloc(10, GFP_KERNEL);
    for (i = MIN_PORT; i <= MAX_PORT; i++) {
        sprintf(name, "tcp-%d", i);
        remove_proc_entry(name, dir_entry);
        printk(KERN_INFO FIGGER_MODULE "/proc/%s removed\n", name);

        sprintf(name, "udp-%d", i);
        remove_proc_entry(name, dir_entry);
        printk(KERN_INFO FIGGER_MODULE "/proc/%s removed\n", name);
    }

    // remove proc dir
    remove_proc_entry(PROC_DIR, NULL);
}

void init_port_list(void) {
    port_list = (struct port_t *)kcalloc(2 * MAX_PORT_NUM,
                                         sizeof(struct port_t), GFP_KERNEL);
    int i;
    for (i = 0; i < 2 * MAX_PORT_NUM; i++) {
        mutex_init(&port_list[i].poll_mutex);
        init_waitqueue_head(&port_list[i].wq_head);
    }
}

void rm_port_list(void) {
    int i;
    for (i = 0; i < 2 * MAX_PORT_NUM; i++) {
        mutex_destroy(&port_list[i].poll_mutex);
    }
    kfree(port_list);
}

static int tcp_interceptor_init(void) {
    create_batch_proc();
    init_port_list();
    register_nf_hook();

    return 0;
}

static void tcp_interceptor_exit(void) {
    remove_batch_proc();
    rm_port_list();
    rm_nf_hook();
}

module_init(tcp_interceptor_init);
module_exit(tcp_interceptor_exit);

MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("Hello Module");
MODULE_AUTHOR("SGG");