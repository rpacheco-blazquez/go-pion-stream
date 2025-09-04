from scapy.all import *

# Lee el paquete RTP crudo
data = open("first_rtp_packet.bin", "rb").read()

# Crea un paquete Ethernet/IP/UDP con el payload RTP
test_pkt = Ether()/IP(src="127.0.0.1", dst="127.0.0.1")/UDP(sport=50000, dport=5004)/Raw(load=data)

# Guarda el paquete en un archivo PCAP
wrpcap("rtp_packet.pcap", [test_pkt])

print("Archivo rtp_packet.pcap generado. Ábrelo con Wireshark y decódealo como RTP.")
