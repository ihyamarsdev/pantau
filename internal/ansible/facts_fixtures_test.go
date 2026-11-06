package ansible

// Facts of a real ansible-core 2.21.4 `setup` run, reduced to the keys SummarizeFacts reads.
const setupFactsJSON = `{
 "plays": [
  {
   "tasks": [
    {
     "hosts": {
      "web-1": {
       "ansible_facts": {
        "ansible_architecture": "x86_64",
        "ansible_default_ipv4": {
         "address": "192.168.110.244",
         "alias": "wlan0",
         "broadcast": "192.168.110.255",
         "gateway": "192.168.110.1",
         "interface": "wlan0",
         "macaddress": "d4:25:8b:ef:c6:9e",
         "mtu": 1500,
         "netmask": "255.255.255.0",
         "network": "192.168.110.0",
         "prefix": "24",
         "type": "ether"
        },
        "ansible_distribution": "Archlinux",
        "ansible_distribution_version": "rolling",
        "ansible_fqdn": "uya",
        "ansible_hostname": "uya",
        "ansible_interfaces": [
         "wt0",
         "veth4f73f97",
         "docker0",
         "veth5b144b2",
         "vethc6bde55",
         "br-6db44801326c",
         "br-4f51765958c6",
         "veth1b8b61c",
         "vethef1e06a",
         "veth2097415",
         "veth74a02ce",
         "lo",
         "veth50c55e9",
         "tailscale0",
         "veth61db5ae",
         "br-04823c00e41b",
         "enp0s31f6",
         "br-811d0b47c2c3",
         "veth010b05f",
         "wlan0",
         "br-ec0240b50382"
        ],
        "ansible_kernel": "7.2.8-1-cachyos",
        "ansible_lo": {
         "active": true,
         "device": "lo",
         "ipv4": {
          "address": "127.0.0.1",
          "macaddress": "00:00:00:00:00:00"
         },
         "ipv6": [
          {
           "address": "::1"
          }
         ],
         "mtu": 65536,
         "type": "loopback"
        },
        "ansible_memtotal_mb": 15748,
        "ansible_mounts": [
         {
          "block_available": 46026579,
          "block_size": 4096,
          "block_total": 124949581,
          "block_used": 78923002,
          "device": "/dev/sda2",
          "dump": 0,
          "fstype": "btrfs",
          "inode_available": 0,
          "inode_total": 0,
          "inode_used": 0,
          "mount": "/",
          "options": "rw,noatime,compress=zstd:3,ssd,discard=async,space_cache=v2,commit=120,subvolid=256,subvol=/@,bind",
          "passno": 0,
          "size_available": 188524867584,
          "size_total": 511793483776,
          "uuid": "69f1b98d-4ef5-4fdc-be0c-033818569ee7"
         }
        ],
        "ansible_os_family": "Archlinux",
        "ansible_processor": [
         "0",
         "GenuineIntel",
         "Intel(R) Core(TM) i7-7500U CPU @ 2.70GHz",
         "1",
         "GenuineIntel",
         "Intel(R) Core(TM) i7-7500U CPU @ 2.70GHz",
         "2",
         "GenuineIntel",
         "Intel(R) Core(TM) i7-7500U CPU @ 2.70GHz",
         "3",
         "GenuineIntel",
         "Intel(R) Core(TM) i7-7500U CPU @ 2.70GHz"
        ],
        "ansible_processor_vcpus": 4,
        "ansible_python_version": "3.14.7",
        "ansible_swaptotal_mb": 15747,
        "ansible_uptime_seconds": 43782,
        "ansible_virtualization_type": "kvm",
        "ansible_wlan0": {
         "active": true,
         "device": "wlan0",
         "features": {
          "esp_hw_offload": "off [fixed]",
          "esp_tx_csum_hw_offload": "off [fixed]",
          "generic_receive_offload": "on",
          "generic_segmentation_offload": "on",
          "highdma": "on",
          "hsr_dup_offload": "off [fixed]",
          "hsr_fwd_offload": "off [fixed]",
          "hsr_tag_ins_offload": "off [fixed]",
          "hsr_tag_rm_offload": "off [fixed]",
          "hw_tc_offload": "off [fixed]",
          "l2_fwd_offload": "off [fixed]",
          "large_receive_offload": "off [fixed]",
          "loopback": "off [fixed]",
          "macsec_hw_offload": "off [fixed]",
          "ntuple_filters": "off [fixed]",
          "receive_hashing": "off [fixed]",
          "rx_all": "off [fixed]",
          "rx_checksumming": "off [fixed]",
          "rx_fcs": "off [fixed]",
          "rx_gro_hw": "off [fixed]",
          "rx_gro_list": "off",
          "rx_udp_gro_forwarding": "off",
          "rx_udp_tunnel_port_offload": "off [fixed]",
          "rx_vlan_filter": "off [fixed]",
          "rx_vlan_offload": "off [fixed]",
          "rx_vlan_stag_filter": "off [fixed]",
          "rx_vlan_stag_hw_parse": "off [fixed]",
          "scatter_gather": "on",
          "tcp_segmentation_offload": "off",
          "tls_hw_rx_offload": "off [fixed]",
          "tls_hw_tx_offload": "off [fixed]",
          "tx_checksum_fcoe_crc": "off [fixed]",
          "tx_checksum_ip_generic": "off [fixed]",
          "tx_checksum_ipv4": "off [fixed]",
          "tx_checksum_ipv6": "off [fixed]",
          "tx_checksum_sctp": "off [fixed]",
          "tx_checksumming": "off",
          "tx_esp_segmentation": "off [fixed]",
          "tx_fcoe_segmentation": "off [fixed]",
          "tx_gre_csum_segmentation": "off [fixed]",
          "tx_gre_segmentation": "off [fixed]",
          "tx_gso_list": "off [fixed]",
          "tx_gso_partial": "off [fixed]",
          "tx_gso_robust": "off [fixed]",
          "tx_ipxip4_segmentation": "off [fixed]",
          "tx_ipxip6_segmentation": "off [fixed]",
          "tx_nocache_copy": "off",
          "tx_scatter_gather": "on",
          "tx_scatter_gather_fraglist": "off [fixed]",
          "tx_sctp_segmentation": "off [fixed]",
          "tx_tcp6_segmentation": "off [fixed]",
          "tx_tcp_accecn_segmentation": "off [fixed]",
          "tx_tcp_ecn_segmentation": "off [fixed]",
          "tx_tcp_mangleid_segmentation": "off [fixed]",
          "tx_tcp_segmentation": "off [fixed]",
          "tx_tunnel_remcsum_segmentation": "off [fixed]",
          "tx_udp_segmentation": "off [fixed]",
          "tx_udp_tnl_csum_segmentation": "off [fixed]",
          "tx_udp_tnl_segmentation": "off [fixed]",
          "tx_vlan_offload": "off [fixed]",
          "tx_vlan_stag_hw_insert": "off [fixed]",
          "vlan_challenged": "off [fixed]"
         },
         "hw_timestamp_filters": [],
         "ipv4": {
          "address": "192.168.110.244",
          "broadcast": "192.168.110.255",
          "netmask": "255.255.255.0",
          "network": "192.168.110.0",
          "prefix": "24"
         },
         "ipv6": [
          {
           "address": "fe80::e0a2:4be5:5562:1333",
           "prefix": "64",
           "scope": "link"
          }
         ],
         "macaddress": "d4:25:8b:ef:c6:9e",
         "module": "iwlwifi",
         "mtu": 1500,
         "pciid": "0000:04:00.0",
         "promisc": false,
         "timestamping": [],
         "type": "ether"
        }
       }
      }
     }
    }
   ]
  }
 ]
}`
