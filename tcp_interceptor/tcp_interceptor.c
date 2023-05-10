#include <asm/uaccess.h>
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

#define BUFFER_SIZE 128

#define PROC_NAME "hello"
#define MESSAGE "Hello World\n"

struct data {
    wait_queue_head_t wq_head;
    struct mutex poll_mutex;
} dev;

static int proc_ok;

static ssize_t proc_read(struct file *file, char __user *usr_buf, size_t count,
                         loff_t *pos) {
    int rv = 0;
    char buffer[BUFFER_SIZE];
    static int completed = 0;

    if (completed) {
        completed = 0;
        return 0;
    }

    completed = 1;

    rv = sprintf(buffer, "Hello World\n");

    // copies the contents of buffer to userspace usr_buf
    copy_to_user(usr_buf, buffer, rv);

    return rv;
}

static __poll_t proc_poll(struct file *file, struct poll_table_struct *wait) {
    unsigned long ino = file_inode(file)->i_ino;
    printk(KERN_INFO "proc_poll called with inode %lu\n", ino);

    unsigned int mask = 0;

    mutex_lock(&dev.poll_mutex);

    poll_wait(file, &dev.wq_head, wait);
    if (proc_ok) mask |= POLLIN | POLLRDNORM;
    mutex_unlock(&dev.poll_mutex);

    return mask;
}

static struct proc_ops proc_ops = {
    .proc_read = proc_read,
    .proc_poll = proc_poll,
};

static int nf_handler(void *priv, struct sk_buff *skb,
                      const struct nf_hook_state *state) {
    if (!skb) return NF_ACCEPT;

    struct iphdr *ip_header = (struct iphdr *)skb_network_header(skb);
    if (ip_header->protocol == IPPROTO_TCP) {
        struct tcphdr *tcp_header = (struct tcphdr *)skb_transport_header(skb);
        if (tcp_header->dest == htons(80)) {
            printk(KERN_INFO "TCP packet to port 80\n");
        }
    } else if (ip_header->protocol == IPPROTO_UDP) {
        struct udphdr *udp_header = (struct udphdr *)skb_transport_header(skb);
        if (udp_header->dest == htons(53)) {
            printk(KERN_INFO "UDP packet to port 53\n");
        }
    } else if (ip_header->protocol == IPPROTO_ICMP) {
        printk(KERN_INFO "ICMP packet\n");
        proc_ok = 1;
        wake_up_interruptible(&dev.wq_head);
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

static int tcp_interceptor_init(void) {
    mutex_init(&dev.poll_mutex);
    init_waitqueue_head(&dev.wq_head);
    proc_create(PROC_NAME, 0, NULL, &proc_ops);
    printk(KERN_INFO "/proc/%s created\n", PROC_NAME);

    register_nf_hook();

    return 0;
}

static void tcp_interceptor_exit(void) {
    remove_proc_entry(PROC_NAME, NULL);
    rm_nf_hook();

    printk(KERN_INFO "/proc/%s removed\n", PROC_NAME);
}

/* Macros for registering module entry and exit points. */
module_init(tcp_interceptor_init);
module_exit(tcp_interceptor_exit);

MODULE_LICENSE("GPL");
MODULE_DESCRIPTION("Hello Module");
MODULE_AUTHOR("SGG");